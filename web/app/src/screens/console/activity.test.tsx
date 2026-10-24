import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { DecisionKind, DecisionSchema, DecisionState } from "../../gen/ptah/console/v1/decisions_pb"
import { renderConsole, syncedMigration, waitingSchema } from "../../test/console"

const minutesAgo = (minutes: number) => timestampFromDate(new Date(Date.now() - minutes * 60_000))

function journaled() {
  // Every instant from one reading of the clock, so the Apply takes exactly
  // the minute between its start and its end.
  const anchor = Date.now()
  const minutesAgo = (minutes: number) => timestampFromDate(new Date(anchor - minutes * 60_000))
  return waitingSchema((schema) => {
    schema.journal = {
      $typeName: "ptah.console.v1.Journal",
      coveredSince: minutesAgo(3 * 24 * 60),
      entries: [],
    }
    schema.journal.entries = [
      { type: "PlanPublished", at: minutesAgo(50), plan: "ptah-plan-a", statements: 2 },
      {
        type: "ApplyFinished",
        at: minutesAgo(40),
        startedAt: minutesAgo(41),
        plan: "ptah-plan-a",
        outcome: "Succeeded",
      },
      { type: "Verified", at: minutesAgo(39), plan: "ptah-plan-a", outcome: "InSync" },
      {
        type: "OperationFailed",
        at: minutesAgo(2 * 24 * 60),
        operation: "Observe",
        reason: "DatabaseUnreachable",
      },
    ].map((entry) => ({ $typeName: "ptah.console.v1.JournalEntry", ...emptyEntry, ...entry }))
  })
}

const emptyEntry = {
  type: "",
  count: 0,
  plan: "",
  statements: 0,
  destructive: false,
  approval: "",
  approver: "",
  outcome: "",
  operation: "",
  reason: "",
  appliedVersions: 0,
  acknowledgment: "",
  acknowledgedBy: "",
}

const approved = create(DecisionSchema, {
  kind: DecisionKind.SCHEMA_APPROVAL,
  ref: { namespace: "application", name: "approve-storefront" },
  ownerName: "storefront",
  planName: "ptah-plan-a",
  decidedBy: "alice",
  decidedAt: minutesAgo(45),
  state: DecisionState.CONSUMED,
})

describe("Activity", () => {
  it("lists what the journals and the decisions say happened, newest first", async () => {
    const { container } = renderConsole("/console/activity", {
      schemas: [journaled()],
      migrations: [syncedMigration()],
      decisions: [approved],
    })

    const list = await screen.findByRole("region", { name: "Entries" })
    const titles = within(list)
      .getAllByRole("listitem")
      .map((item) => item.querySelector("b")?.textContent)
    expect(titles).toEqual([
      "Verified in sync",
      "Apply succeeded",
      "Plan approved by alice",
      "Plan published",
      "Observe failed: DatabaseUnreachable",
    ])
    expect(within(list).getByText(/took 1 min/)).toBeVisible()
    expect(screen.getByText(/Complete since/)).toHaveTextContent("for every resource read")
    expect(screen.getByText(/Complete since/)).toHaveTextContent(
      "1 resource runs under an operator that keeps no journal."
    )
    expect(await axe(container)).toHaveNoViolations()
  })

  it("filters by type, period and person, and keeps the filters in the address", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console/activity", {
      schemas: [journaled()],
      migrations: [],
      decisions: [approved],
    })
    const list = await screen.findByRole("region", { name: "Entries" })

    await user.click(
      within(screen.getByRole("group", { name: "Type" })).getByRole("button", { name: /^Failures/ })
    )
    expect(within(list).getAllByRole("listitem")).toHaveLength(1)
    expect(history.location.search).toContain("type=failure")

    await user.click(
      within(screen.getByRole("group", { name: "Period" })).getByRole("button", {
        name: "Last hour",
      })
    )
    expect(screen.getByText("No entries match these filters")).toBeVisible()

    await user.click(
      within(screen.getByRole("group", { name: "Type" })).getByRole("button", { name: /^All/ })
    )
    await user.click(
      within(screen.getByRole("group", { name: "Who" })).getByRole("button", { name: "alice" })
    )
    const shown = await screen.findByRole("region", { name: "Entries" })
    expect(within(shown).getAllByRole("listitem")).toHaveLength(1)
    expect(within(shown).getByText("Plan approved by alice")).toBeVisible()
    expect(history.location.search).toContain("who=alice")
  })

  it("opens an entry to say what it was about, and links the resource", async () => {
    const user = userEvent.setup()
    renderConsole("/console/activity", { schemas: [journaled()], migrations: [] })

    const list = await screen.findByRole("region", { name: "Entries" })
    await user.click(within(list).getByText("Plan published"))

    expect(within(list).getByText("2 statements")).toBeVisible()
    expect(
      within(list).getAllByRole("link", { name: "Open application/storefront" })[0]
    ).toHaveAttribute("href", "/console/schemas/application/storefront")
  })

  it("still lists the journals when decisions may not be listed, and says so", async () => {
    renderConsole("/console/activity", {
      schemas: [journaled()],
      migrations: [],
      decisionsRefused: true,
    })

    expect(
      await screen.findByText(/Approvals and acknowledgments are not listed: .*may not list/)
    ).toBeVisible()
    expect(screen.getByText("Apply succeeded")).toBeVisible()
  })

  it("says when nothing is kept yet", async () => {
    renderConsole("/console/activity", { schemas: [waitingSchema()], migrations: [] })

    expect(await screen.findByText("Nothing kept yet")).toBeVisible()
    expect(
      screen.getByText(
        "The operator these resources run under keeps no journal, so only decisions are listed."
      )
    ).toBeVisible()
  })

  it("is in the console's navigation", async () => {
    renderConsole("/console", { schemas: [], migrations: [] })

    const nav = await screen.findByRole("navigation", { name: "Console" })
    expect(within(nav).getByRole("link", { name: "Activity" })).toHaveAttribute(
      "href",
      "/console/activity"
    )
  })
})
