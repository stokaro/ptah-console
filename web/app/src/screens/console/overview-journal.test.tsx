import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  Access,
  JournalEntrySchema,
  JournalSchema,
  type JournalEntrySchema as EntrySchema,
} from "../../gen/ptah/console/v1/resources_pb"
import { renderConsole, syncedMigration, waitingSchema } from "../../test/console"
import type { MessageInitShape } from "@bufbuild/protobuf"

const minutesAgo = (minutes: number) => timestampFromDate(new Date(Date.now() - minutes * 60_000))

function withJournal(
  entries: MessageInitShape<typeof EntrySchema>[],
  coveredMinutesAgo = 3 * 24 * 60
) {
  return waitingSchema((schema) => {
    schema.journal = create(JournalSchema, {
      coveredSince: minutesAgo(coveredMinutesAgo),
      entries: entries.map((entry) => create(JournalEntrySchema, entry)),
    })
  })
}

const busy = () =>
  withJournal([
    { type: "PlanPublished", at: minutesAgo(30 * 60), plan: "ptah-plan-a", statements: 1 },
    {
      type: "ApplyFinished",
      at: minutesAgo(29 * 60),
      startedAt: minutesAgo(29 * 60 + 1),
      outcome: "Succeeded",
    },
    { type: "Verified", at: minutesAgo(20), outcome: "InSync" },
    {
      type: "OperationFailed",
      at: minutesAgo(15),
      operation: "Observe",
      reason: "DatabaseUnreachable",
    },
  ])

describe("The overview, from the journals", () => {
  it("says on the tiles what moved in the last 24 hours", async () => {
    renderConsole("/console", { schemas: [busy()], migrations: [] })

    const tiles = await screen.findByRole("navigation", { name: "Resources by state" })
    expect(within(tiles).getByRole("link", { name: /^Needs attention/ })).toHaveTextContent(
      "1 failure in the last 24 h"
    )
    expect(within(tiles).getByRole("link", { name: /^In progress/ })).toHaveTextContent(
      "0 Applies and runs in the last 24 h"
    )
    expect(within(tiles).getByRole("link", { name: /^In sync/ })).toHaveTextContent(
      "1 verified in sync in the last 24 h"
    )
  })

  it("counts plans and executions per day over the days the journals cover", async () => {
    const { container } = renderConsole("/console", { schemas: [busy()], migrations: [] })

    const section = await screen.findByRole("region", { name: "Plans and executions" })
    expect(section).toHaveTextContent("Per UTC day, over the")
    const table = within(section).getByRole("table", { name: "Plans and executions per UTC day" })
    const totals = within(table)
      .getAllByRole("row")
      .slice(1)
      .map((row) =>
        within(row)
          .getAllByRole("cell")
          .reduce((sum, cell) => sum + Number(cell.textContent), 0)
      )
    expect(totals.reduce((sum, n) => sum + n, 0)).toBe(2)
    expect(await axe(container)).toHaveNoViolations()
  })

  it("says today counts only from when the journals became complete", async () => {
    renderConsole("/console", { schemas: [withJournal([], 5)], migrations: [] })

    const section = await screen.findByRole("region", { name: "Plans and executions" })
    expect(section).toHaveTextContent("so today counts from then")
  })

  it("lists the newest events and leads to all of them", async () => {
    renderConsole("/console", { schemas: [busy()], migrations: [syncedMigration()] })

    const recent = await screen.findByRole("region", { name: "Recent activity" })
    expect(within(recent).getAllByRole("listitem")[0]).toHaveTextContent(
      "Observe failed: DatabaseUnreachable"
    )
    expect(within(recent).getByRole("link", { name: "All activity →" })).toHaveAttribute(
      "href",
      "/console/activity"
    )
  })

  it("falls back to condition changes where no journal is kept", async () => {
    renderConsole("/console", { schemas: [waitingSchema()], migrations: [] })

    const recent = await screen.findByRole("region", { name: "Recent activity" })
    expect(recent).toHaveTextContent("Each condition keeps only its last change")
    expect(screen.getByRole("region", { name: "Plans and executions" })).toHaveTextContent(
      "No journal is kept for these resources"
    )
  })

  it("shows the loop each resource repeats and where it is explained", async () => {
    renderConsole("/console", { schemas: [], migrations: [] })

    const lifecycle = await screen.findByRole("region", { name: "Operator lifecycle" })
    expect(within(lifecycle).getAllByRole("listitem")).toHaveLength(7)
    expect(within(lifecycle).getByRole("link", { name: "How it works →" })).toHaveAttribute(
      "href",
      "/console/architecture"
    )
  })

  it("reads everything again on Refresh", async () => {
    const user = userEvent.setup()
    let reads = 0
    renderConsole("/console", {
      schemas: [busy()],
      migrations: () => {
        reads++
        return []
      },
    })
    await screen.findByRole("region", { name: "Recent activity" })
    const before = reads

    await user.click(screen.getByRole("button", { name: "Refresh" }))

    await waitFor(() => expect(reads).toBeGreaterThan(before))
  })
})

describe("Namespace health", () => {
  it("searches and expands a long list of namespaces", async () => {
    const user = userEvent.setup()
    const others = Array.from({ length: 11 }, (_, i) => ({
      name: `team-${String(i).padStart(2, "0")}`,
      access: Access.FORBIDDEN,
    }))
    renderConsole("/console", { schemas: [], migrations: [], otherNamespaces: others })

    const health = await screen.findByRole("region", { name: "Namespace health" })
    const rows = () => within(within(health).getByRole("table")).getAllByRole("rowheader")
    expect(rows()).toHaveLength(8)
    await user.click(within(health).getByRole("button", { name: "Show all 12" }))
    expect(rows()).toHaveLength(12)
    await user.type(within(health).getByRole("textbox", { name: "Search namespaces" }), "team-1")
    expect(rows().map((row) => row.textContent)).toEqual(["team-10"])
    await user.clear(within(health).getByRole("textbox", { name: "Search namespaces" }))
    await user.type(within(health).getByRole("textbox", { name: "Search namespaces" }), "payroll")
    expect(within(health).getByText("No namespace matches “payroll”.")).toBeVisible()
  })
})
