import { create } from "@bufbuild/protobuf"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import { DecisionOutcome, type ApproveSchemaPlanRequest } from "../gen/ptah/console/v1/decisions_pb"
import { Answer, StatementSchema } from "../gen/ptah/console/v1/resources_pb"
import { WatchedKind } from "../gen/ptah/console/v1/watch_pb"
import {
  heartbeat,
  renderConsole,
  result,
  scriptedWatch,
  storedPlan,
  waitingSchema,
} from "../test/console"

const newer = () =>
  storedPlan((plan) => {
    plan.name = "storefront-p-9d01"
    plan.uid = "uid-newer-plan"
    plan.fingerprint = "sha256:9d0100000000000000000000"
    plan.statements = [
      create(StatementSchema, {
        index: 0,
        sql: "ALTER TABLE customers ADD COLUMN phone TEXT;",
        severity: "safe",
      }),
    ]
  })

const invalidate = () => ({
  event: {
    case: "invalidate" as const,
    value: { kind: WatchedKind.SCHEMA, namespace: "application", name: "storefront" },
  },
})

describe("Reviewing a plan", () => {
  it("opens from a link for a reviewer who may read the schema and not list schemas", async () => {
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      listForbidden: true,
      capabilities: { approveSchemaPlans: Answer.DENIED, readPlanSql: Answer.ALLOWED },
    })

    expect(await screen.findByRole("list", { name: "SQL statements" })).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "storefront-p-8f2c" })).toBeInTheDocument()
  })

  it("approves the plan the person reviewed, even when it changes while the dialog is open", async () => {
    const user = userEvent.setup()
    const watch = scriptedWatch()
    let current = storedPlan()
    const sent: ApproveSchemaPlanRequest[] = []
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      plan: () => current,
      watch: watch.stream,
      approve: (request) => {
        sent.push(request)
        return result({
          outcome: DecisionOutcome.CHANGED,
          message: "A newer plan replaced the one you reviewed. Nothing was approved.",
        })
      },
    })
    watch.push(heartbeat())
    await user.click(await screen.findByRole("button", { name: "Approve this plan…" }))
    const dialog = await screen.findByRole("alertdialog")

    current = newer()
    watch.push(invalidate())
    await screen.findByText("ALTER TABLE customers ADD COLUMN phone TEXT;", undefined, {
      timeout: 3000,
    })
    await user.click(within(dialog).getByRole("checkbox"))
    await user.click(within(dialog).getByRole("button", { name: "Confirm" }))

    expect(await within(dialog).findByText("It changed")).toBeInTheDocument()
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({ planName: "storefront-p-8f2c", planUid: "uid-plan" })
  })

  it("says when the plan being read was replaced, and holds approval until the new one is read", async () => {
    const user = userEvent.setup()
    const watch = scriptedWatch()
    let current = storedPlan()
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      plan: () => current,
      watch: watch.stream,
    })
    watch.push(heartbeat())
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeEnabled()
    )

    current = newer()
    watch.push(invalidate())

    expect(
      await screen.findByText(/A newer plan replaced the one you were reading/)
    ).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
    await user.click(screen.getByRole("button", { name: "I am reading the new plan" }))
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeEnabled()
    )
    expect(screen.queryByText(/A newer plan replaced the one you were reading/)).toBeNull()
  })
})
