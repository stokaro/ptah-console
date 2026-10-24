import { create } from "@bufbuild/protobuf"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { DecisionOutcome } from "../../gen/ptah/console/v1/decisions_pb"
import { Answer, MigrationPlanSchema, SqlAccess } from "../../gen/ptah/console/v1/resources_pb"
import {
  renderConsole,
  result,
  storedPlan,
  syncedMigration,
  waitingSchema,
} from "../../test/console"

const allowedButNoApproval = {
  listSchemas: Answer.ALLOWED,
  listMigrations: Answer.ALLOWED,
  readPlanSql: Answer.ALLOWED,
  approveSchemaPlans: Answer.DENIED,
}

function checks() {
  return within(screen.getByRole("list", { name: "Checks before approving" }))
    .getAllByRole("listitem")
    .map((item) => item.textContent)
}

describe("The full console's plan review", () => {
  it("shows what the plan changes, its statement types, its integrity and its exact SQL", async () => {
    const { container } = renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
    })

    expect(await screen.findByRole("heading", { level: 1, name: "Plan review" })).toBeVisible()
    const types = await screen.findByRole("region", { name: "Statement types" })
    expect(types).toHaveTextContent("drops a column1")
    expect(types).toHaveTextContent("grants1")
    expect(types).toHaveTextContent("No mark1")
    expect(screen.getByRole("region", { name: "Integrity" })).toHaveTextContent(
      "Verified2 chunks, each checked by size, digest and order"
    )
    expect(screen.getByText("3 statements, numbered as the plan runs them.")).toBeVisible()
    expect(checks().every((text) => !text?.includes("may not"))).toBe(true)
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeEnabled()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("filters the SQL and keeps the plan's numbering", async () => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
    })

    await user.type(await screen.findByRole("textbox", { name: "Filter the statements" }), "grant")
    expect(
      screen.getByText("Showing 1 of 3 statements, numbered as the plan runs them.")
    ).toBeVisible()
    await user.clear(screen.getByRole("textbox", { name: "Filter the statements" }))
    await user.click(
      within(screen.getByRole("group", { name: "Statements" })).getByRole("button", {
        name: /^Marked/,
      })
    )
    expect(
      screen.getByText("Showing 2 of 3 statements, numbered as the plan runs them.")
    ).toBeVisible()
  })

  it("opens the approval for exactly the plan on the page", async () => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
    })

    await user.click(await screen.findByRole("button", { name: "Approve this plan…" }))

    expect(await screen.findByRole("alertdialog")).toHaveTextContent("storefront-p-8f2c")
  })

  it.each([
    {
      outcome: DecisionOutcome.ADMITTED,
      link: "Follow it",
      href: "/console/schemas/application/storefront",
    },
    {
      outcome: DecisionOutcome.CHANGED,
      link: "Review the new plan",
      href: "/console/schemas/application/storefront/plan",
    },
  ])("leads on to the full console's own page once the answer is $link", async (row) => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
      approve: () => result({ outcome: row.outcome }),
    })
    await user.click(await screen.findByRole("button", { name: "Approve this plan…" }))
    const dialog = await screen.findByRole("alertdialog")
    await user.click(
      within(dialog).getByRole("checkbox", { name: "I reviewed what this changes." })
    )
    const confirm = within(dialog).getByRole("button", { name: "Confirm" })
    await waitFor(() => expect(confirm).toBeEnabled())

    await user.click(confirm)

    expect(await within(dialog).findByRole("link", { name: row.link })).toHaveAttribute(
      "href",
      row.href
    )
  })

  it("says when the plan a link named is no longer current, and approves nothing from it", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole(
      "/console/schemas/application/storefront/plan?plan=storefront-p-0ld1",
      { schemas: [waitingSchema()], migrations: [] }
    )

    const removed = await screen.findByRole("region", { name: "Plan no longer current" })
    expect(removed).toHaveTextContent("storefront-p-8f2c replaced it")
    expect(removed).toHaveTextContent("Neither the resource's journal nor the decisions")
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
    expect(checks()[0]).toContain("The plan you read is no longer current")

    await user.click(within(removed).getByRole("button", { name: "Read the current plan" }))

    await waitFor(() => expect(history.location.search).not.toContain("plan="))
    expect(screen.queryByRole("region", { name: "Plan no longer current" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeEnabled()
  })

  it("says the SQL is not checked by an identity that may not read the chunks", async () => {
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
      plan: storedPlan((plan) => {
        plan.sqlAccess = SqlAccess.NOT_GRANTED
        plan.statements = []
        plan.sqlProblem = "This identity may not get ptahschemaplanchunks."
      }),
    })

    expect(await screen.findByRole("region", { name: "Integrity" })).toHaveTextContent(
      "Not checked by this identity"
    )
    expect(screen.getByRole("region", { name: "Statement types" })).toHaveTextContent(
      "Not available: the SQL is not shown."
    )
    expect(screen.getByText("This identity may not get ptahschemaplanchunks.")).toBeVisible()
  })

  it("offers no approval to an identity that may not approve, and says why", async () => {
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [waitingSchema()],
      migrations: [],
      capabilities: allowedButNoApproval,
    })

    await screen.findByRole("heading", { level: 1, name: "Plan review" })
    expect(
      checks().some((text) =>
        text?.endsWith(
          "This identity may not create approvals in applicationShare the link with an approver."
        )
      )
    ).toBe(true)
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
  })

  it("reviews a migration plan by its files, without a contract it does not read", async () => {
    const waiting = syncedMigration((m) => {
      m.phase = "AwaitingApproval"
      m.planName = "billing-p-1"
      m.planUid = "uid-billing-plan"
    })
    renderConsole("/console/migrations/application/billing/plan", {
      schemas: [],
      migrations: [waiting],
      migrationPlan: create(MigrationPlanSchema, {
        name: "billing-p-1",
        uid: "uid-billing-plan",
        fingerprint: "sha256:aa",
        contractVersion: 1,
        migrations: [
          { version: 2n, versionKey: "2", description: "add totals", checksum: "h1:aa" },
        ],
      }),
    })

    expect(await screen.findByRole("table", { name: "Steps in order" })).toHaveTextContent(
      "add totals"
    )
    expect(checks().some((text) => text?.includes("contract"))).toBe(false)
  })
})
