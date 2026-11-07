import { Code, ConnectError } from "@connectrpc/connect"
import { create } from "@bufbuild/protobuf"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"

import {
  AttentionKind,
  AttentionSchema,
  MigrationPlanSchema,
  SqlAccess,
  type SchemaPlan,
} from "../gen/ptah/console/v1/resources_pb"
import { renderConsole, storedPlan, syncedMigration, waitingSchema } from "../test/console"

const unread = (access: SqlAccess) =>
  storedPlan((plan) => {
    plan.sqlAccess = access
    plan.statements = []
    plan.sqlProblem = "What the plan reader said."
  })

const failedToLoad = () => {
  throw new ConnectError("the API server did not return the plan's SQL", Code.Unavailable)
}

/** Each way a schema plan can fail to reach the page whole, and what is said of it. */
const refused: [string, () => SchemaPlan, RegExp][] = [
  ["UNSUPPORTED", () => unread(SqlAccess.UNSUPPORTED), /does not read/],
  ["INTEGRITY_FAILED", () => unread(SqlAccess.INTEGRITY_FAILED), /failed verification/],
  ["NOT_GRANTED", () => unread(SqlAccess.NOT_GRANTED), /may not read/],
  ["a plan that failed to load", failedToLoad, /could not read/],
]

const waitingMigration = () =>
  syncedMigration((migration) => {
    migration.phase = "AwaitingApproval"
    migration.approvalOpen = true
    migration.planName = "billing-p-1"
    migration.planUid = "uid-plan"
    migration.attention = [create(AttentionSchema, { kind: AttentionKind.WAITING_FOR_APPROVAL })]
  })

const readableMigrationPlan = create(MigrationPlanSchema, {
  namespace: "application",
  migration: "billing",
  name: "billing-p-1",
  uid: "uid-plan",
  fingerprint: "sha256:bb",
  contractVersion: 1,
  migrations: [{ version: 14n, versionKey: "14", description: "add invoices", checksum: "aa" }],
})

describe("An approval of a plan the console could not read", () => {
  describe.each(refused)("%s", (_, plan, says) => {
    it("is not offered on the focused plan page", async () => {
      renderConsole("/approve/application/schema/storefront", {
        schemas: [waitingSchema()],
        plan,
      })

      expect(
        await screen.findByRole("status", { name: "Why this plan cannot be approved" })
      ).toHaveTextContent(says)
      expect(screen.queryByRole("button", { name: "Approve this plan…" })).toBeNull()
    })

    it("fails a check on the full console's plan page", async () => {
      renderConsole("/console/schemas/application/storefront/plan", {
        schemas: [waitingSchema()],
        migrations: [],
        plan,
      })

      const checks = await screen.findByRole("list", { name: "Checks before approving" })
      await waitFor(() => expect(checks).toHaveTextContent(says))
      expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
    })

    it("cannot be confirmed from the queue, which reads the plan only in the dialog", async () => {
      const user = userEvent.setup()
      const sent: unknown[] = []
      renderConsole("/approve", {
        schemas: [waitingSchema()],
        plan,
        approve: (request) => {
          sent.push(request)
          throw new Error("nothing may be sent")
        },
      })

      await user.click(await screen.findByRole("button", { name: "Approve…" }))
      const dialog = await screen.findByRole("alertdialog")
      await waitFor(() => expect(dialog).toHaveTextContent(says))
      await user.click(
        within(dialog).getByRole("checkbox", { name: "I reviewed what this changes." })
      )

      expect(within(dialog).getByRole("button", { name: "Confirm" })).toBeDisabled()
      expect(sent).toEqual([])
    })
  })

  it("is offered once the plan was read and verified", async () => {
    renderConsole("/approve/application/schema/storefront", {
      schemas: [waitingSchema()],
      plan: () => unread(SqlAccess.AVAILABLE),
    })

    const approve = await screen.findByRole("button", { name: "Approve this plan…" })
    await waitFor(() => expect(approve).toBeEnabled())
    expect(screen.queryByRole("status", { name: "Why this plan cannot be approved" })).toBeNull()
  })

  it("passes the check on the full console's plan page once it was read and verified", async () => {
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
    })

    const checks = await screen.findByRole("list", { name: "Checks before approving" })
    await waitFor(() => expect(checks).toHaveTextContent("The console read and verified this plan"))
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeEnabled()
  })

  it("can be confirmed from the queue once it was read and verified", async () => {
    const user = userEvent.setup()
    renderConsole("/approve", { schemas: [waitingSchema()] })

    await user.click(await screen.findByRole("button", { name: "Approve…" }))
    const dialog = await screen.findByRole("alertdialog")
    await user.click(
      within(dialog).getByRole("checkbox", { name: "I reviewed what this changes." })
    )

    await waitFor(() =>
      expect(within(dialog).getByRole("button", { name: "Confirm" })).toBeEnabled()
    )
  })
})

describe("An approval of a migration plan", () => {
  const notRead = new ConnectError(
    "the plan under that name is not the one the migration records",
    Code.FailedPrecondition
  )

  it("is not offered on the focused page when the plan does not read", async () => {
    renderConsole("/approve/application/migration/billing", {
      schemas: [],
      migrations: [waitingMigration()],
      migrationPlan: notRead,
    })

    expect(
      await screen.findByRole("status", { name: "Why this plan cannot be approved" })
    ).toHaveTextContent(/could not read/)
    expect(screen.queryByRole("button", { name: "Approve this plan…" })).toBeNull()
  })

  it("fails a check on the full console's page when the plan does not read", async () => {
    renderConsole("/console/migrations/application/billing/plan", {
      schemas: [],
      migrations: [waitingMigration()],
      migrationPlan: notRead,
    })

    const checks = await screen.findByRole("list", { name: "Checks before approving" })
    await waitFor(() => expect(checks).toHaveTextContent(/could not read/))
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
  })

  it("is offered when the plan reads", async () => {
    renderConsole("/console/migrations/application/billing/plan", {
      schemas: [],
      migrations: [waitingMigration()],
      migrationPlan: readableMigrationPlan,
    })

    const checks = await screen.findByRole("list", { name: "Checks before approving" })
    await waitFor(() => expect(checks).toHaveTextContent("The console read this plan's steps"))
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeEnabled()
  })
})
