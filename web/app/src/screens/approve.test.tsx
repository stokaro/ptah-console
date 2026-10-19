import { create } from "@bufbuild/protobuf"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  DecisionKind,
  DecisionOutcome,
  DecisionSchema,
  type ApproveSchemaPlanRequest,
} from "../gen/ptah/console/v1/decisions_pb"
import { Answer } from "../gen/ptah/console/v1/resources_pb"
import { renderConsole, result, waitingSchema } from "../test/console"

const admitted = (request: ApproveSchemaPlanRequest) =>
  result({
    outcome: DecisionOutcome.ADMITTED,
    decision: create(DecisionSchema, {
      kind: DecisionKind.SCHEMA_APPROVAL,
      planName: request.planName,
      planUid: request.planUid,
      planFingerprint: request.planFingerprint,
      decidedBy: "alice",
    }),
  })

async function approveFromReview(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "Approve this plan…" }))
  const dialog = await screen.findByRole("alertdialog")
  const confirm = within(dialog).getByRole("button", { name: "Confirm" })
  expect(confirm).toBeDisabled()
  await user.click(within(dialog).getByRole("checkbox", { name: "I reviewed what this changes." }))
  await waitFor(() => expect(confirm).toBeEnabled())
  await user.click(confirm)
  return dialog
}

describe("Approver", () => {
  it("lists the plan that waits, with what it changes", async () => {
    const { container } = renderConsole("/approve", { schemas: [waitingSchema()] })

    const list = await screen.findByRole("list", { name: "Plans waiting for approval" })
    expect(within(list).getByRole("link", { name: "storefront-p-8f2c" })).toBeInTheDocument()
    expect(
      within(list).getByText("Destructive: removes or rewrites data, as reported by the plan")
    ).toBeInTheDocument()
    expect(within(list).getByText("Changes privileges: GRANT")).toBeInTheDocument()
    expect(screen.getByText("alice")).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("says nothing waits when nothing does", async () => {
    renderConsole("/approve", { schemas: [] })

    expect(
      await screen.findByRole("heading", { name: "Nothing waits for you" })
    ).toBeInTheDocument()
  })

  it("shows the exact SQL as text and sends exactly the reviewed plan", async () => {
    const user = userEvent.setup()
    const sent: ApproveSchemaPlanRequest[] = []
    const { container } = renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      approve: (request) => {
        sent.push(request)
        return admitted(request)
      },
    })

    const sql = await screen.findByRole("list", { name: "SQL statements" })
    expect(within(sql).getByText(/<script>alert\(1\)<\/script>/)).toBeInTheDocument()
    expect(container.querySelector("script")).toBeNull()
    expect(within(sql).getByText("drops a column")).toBeInTheDocument()
    expect(screen.getByText("SQL verified · 2 of 2 chunks · digests match")).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()

    const dialog = await approveFromReview(user)

    expect(await within(dialog).findByText("Approval admitted")).toBeInTheDocument()
    expect(within(dialog).getByText(/recorded by admission as alice/)).toBeInTheDocument()
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({
      namespace: "application",
      schemaName: "storefront",
      schemaUid: "uid-schema",
      planName: "storefront-p-8f2c",
      planUid: "uid-plan",
      planFingerprint: "sha256:9c2400000000000000007b10",
      contractVersion: 1,
    })
  })

  it.each([
    {
      outcome: DecisionOutcome.CHANGED,
      message: "A newer plan replaced the one you reviewed. Nothing was approved.",
      title: "It changed",
    },
    {
      outcome: DecisionOutcome.FORBIDDEN,
      message: 'User "alice" cannot create resource "ptahschemaapprovals"',
      title: "Kubernetes does not let you",
    },
    {
      outcome: DecisionOutcome.REFUSED,
      message:
        'admission webhook "vptahschemaapproval.operator.ptah.run" denied the request: the author may not approve',
      title: "Refused",
    },
  ])("reports $title with the server's own words", async ({ outcome, message, title }) => {
    const user = userEvent.setup()
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      approve: () => result({ outcome, message }),
    })

    const dialog = await approveFromReview(user)

    expect(await within(dialog).findByText(title)).toBeInTheDocument()
    expect(within(dialog).getByText(message)).toBeInTheDocument()
    expect(within(dialog).queryByText("Approval admitted")).toBeNull()
  })

  it("reads an unknown outcome back by name before saying anything else", async () => {
    const user = userEvent.setup()
    const asked: string[] = []
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      approve: () =>
        result({
          outcome: DecisionOutcome.UNKNOWN,
          message: "The API server did not answer in time.",
        }),
      getDecision: (request) => {
        asked.push(request.name)
        return create(DecisionSchema, { kind: DecisionKind.SCHEMA_APPROVAL, decidedBy: "alice" })
      },
    })

    const dialog = await approveFromReview(user)
    expect(await within(dialog).findByText("Submission outcome unknown")).toBeInTheDocument()
    await user.click(within(dialog).getByRole("button", { name: "Check result" }))

    expect(await within(dialog).findByText("Approval admitted")).toBeInTheDocument()
    expect(asked).toEqual(["ptah-console-0123456789abcdef0123"])
  })

  it("does not offer approval to an identity RBAC does not let approve", async () => {
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      capabilities: { approveSchemaPlans: Answer.DENIED, readPlanSql: Answer.ALLOWED },
    })

    expect(await screen.findByText(/may not approve plans in application/)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Approve this plan…" })).toBeNull()
  })
})
