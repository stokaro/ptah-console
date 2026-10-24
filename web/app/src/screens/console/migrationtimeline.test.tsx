import { create } from "@bufbuild/protobuf"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { Evidence } from "../../gen/ptah/console/v1/diagnostics_pb"
import {
  MigrationHistorySchema,
  MigrationPlanSchema,
  MigrationRevisionSchema,
  MigrationRunSchema,
  type Migration,
} from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, syncedMigration } from "../../test/console"

/** Seventy applied rows, a run that stopped in 71, and 72 waiting in the plan. */
function longHistory(change: (m: Migration) => void = () => {}) {
  return syncedMigration((m) => {
    m.planName = "billing-p-9"
    m.history = create(MigrationHistorySchema, {
      observedAt: minutesAgo(1),
      currentVersion: 70n,
      appliedCount: 70,
      pendingCount: 2,
      revisions: Array.from({ length: 64 }, (_, index) => {
        const version = BigInt(70 - index)
        return create(MigrationRevisionSchema, {
          version,
          description: `step ${version}`,
          checksum: `h1:${version}aaaaaaaaaaaaaaaaaaaa${version}`,
          state: "applied",
          appliedAt: minutesAgo(Number(version)),
          ...(version % 2n === 0n ? { executionMillis: version * 10n } : {}),
        })
      }),
    })
    m.lastRun = create(MigrationRunSchema, {
      outcome: "Failed",
      jobName: "ptah-m-apply-billing-9",
      startedAt: minutesAgo(2),
      finishedAt: minutesAgo(1),
      failure: { version: 71n, statement: 2, statements: 3, sqlState: "42P01" },
    })
    change(m)
  })
}

const longPlan = create(MigrationPlanSchema, {
  namespace: "application",
  migration: "billing",
  name: "billing-p-9",
  migrations: [
    { version: 71n, versionKey: "71", description: "read ledger", checksum: "h1:71" },
    { version: 72n, versionKey: "72", description: "add totals", checksum: "h1:72" },
  ],
})

function timeline() {
  return screen.getByRole("list", { name: "Migrations of billing, newest first" })
}

function versions() {
  return within(timeline())
    .getAllByRole("listitem")
    .map((item) => item.id.replace("step-", ""))
}

describe("A migration's timeline", () => {
  it("lists the newest steps first, a page at a time, and says which rows it does not have", async () => {
    const user = userEvent.setup()
    const { container } = renderConsole("/console/migrations/application/billing", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })

    await screen.findByRole("list", { name: "Migrations of billing, newest first" })
    expect(versions()).toHaveLength(50)
    expect(versions().slice(0, 3)).toEqual(["72", "71", "70"])
    expect(screen.getByText(/The history counts 6 more applied migrations/)).toBeVisible()
    expect(await axe(container)).toHaveNoViolations()

    await user.click(screen.getByRole("button", { name: "Show 16 more of 16 earlier" }))

    expect(versions()).toHaveLength(66)
    expect(versions().at(-1)).toBe("7")
    expect(screen.queryByRole("button", { name: /more of/ })).not.toBeInTheDocument()
  })

  it("searches by version or file name and narrows to a state", async () => {
    const user = userEvent.setup()
    renderConsole("/console/migrations/application/billing", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })
    const filters = await screen.findByRole("group", { name: "Show migrations" })
    expect(
      within(filters)
        .getAllByRole("button")
        .map((button) => button.textContent)
    ).toEqual(["All 66", "Applied 64", "Pending 1", "Needs a person 1"])

    await user.type(screen.getByRole("textbox", { name: "Search the migrations" }), "totals")
    expect(versions()).toEqual(["72"])

    await user.clear(screen.getByRole("textbox", { name: "Search the migrations" }))
    await user.type(screen.getByRole("textbox", { name: "Search the migrations" }), "15")
    expect(versions()).toEqual(["15"])

    await user.clear(screen.getByRole("textbox", { name: "Search the migrations" }))
    await user.click(within(filters).getByRole("button", { name: "Needs a person 1" }))
    expect(versions()).toEqual(["71"])
    expect(within(timeline()).getByRole("listitem")).toHaveTextContent(
      "The last run stopped here: migration 71, statement 2 of 3, SQLSTATE 42P01"
    )
  })

  it("jumps to the step that needs a person from any filter", async () => {
    const user = userEvent.setup()
    renderConsole("/console/migrations/application/billing", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })
    const filters = await screen.findByRole("group", { name: "Show migrations" })
    await user.click(within(filters).getByRole("button", { name: "Pending 1" }))
    expect(versions()).toEqual(["72"])

    await user.click(screen.getByRole("button", { name: "↓ Jump to failed" }))

    expect(versions()).toHaveLength(50)
    await waitFor(() => expect(document.activeElement?.id).toBe("step-71"))
  })
})

describe("A migration's revision rows", () => {
  it("lists each row with its checksum, when it was written and how long it ran", async () => {
    renderConsole("/console/migrations/application/billing", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })

    const table = await screen.findByRole("table", { name: "Revision rows of billing" })
    const rows = within(table).getAllByRole("row").slice(1)
    expect(rows).toHaveLength(64)
    expect(rows[0]).toHaveTextContent("70step 70h1:70aaaa…aaaa70✓applied1 h ago700 ms")
    expect(rows[1]).toHaveTextContent("69step 69h1:69aaaa…aaaa69✓applied1 h ago–")
  })

  it("narrows the rows to a state", async () => {
    const user = userEvent.setup()
    renderConsole("/console/migrations/application/billing", {
      migrations: [
        longHistory((m) => {
          m.history!.revisions[3].state = "modified"
        }),
      ],
      migrationPlan: longPlan,
    })
    const filters = await screen.findByRole("group", { name: "Show rows" })

    await user.click(within(filters).getByRole("button", { name: "modified 1" }))

    const table = screen.getByRole("table", { name: "Revision rows of billing" })
    expect(within(table).getAllByRole("row").slice(1)).toHaveLength(1)
    expect(within(table).getAllByRole("row")[1]).toHaveTextContent("67step 67")
  })

  it("charts how long each timed migration ran, and leaves out the rows that say nothing", async () => {
    renderConsole("/console/migrations/application/billing", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })

    const chart = await screen.findByRole("table", { name: "How long each migration ran" })
    const rows = within(chart).getAllByRole("row").slice(1)
    expect(rows).toHaveLength(25)
    expect(rows.at(-1)).toHaveTextContent("70step 70700 ms")
    expect(rows.every((row) => Number(row.firstChild?.textContent) % 2 === 0)).toBe(true)
  })

  it("says no row records a duration rather than drawing them as instant", async () => {
    renderConsole("/console/migrations/application/billing", {
      migrations: [
        longHistory((m) => {
          for (const row of m.history!.revisions) {
            row.executionMillis = undefined
          }
        }),
      ],
      migrationPlan: longPlan,
    })

    expect(
      await screen.findByText(/No revision row records how long its migration ran/)
    ).toBeVisible()
    expect(
      screen.queryByRole("table", { name: "How long each migration ran" })
    ).not.toBeInTheDocument()
  })
})

describe("A migration's panels", () => {
  it("keeps the open panel in the address, and opens one from it", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console/migrations/application/billing", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
      diagnostics: {
        events: {
          evidence: Evidence.SHOWN,
          events: [
            {
              type: "Warning",
              reason: "RunFailed",
              message: "Migration 71 failed",
              count: 2,
              lastSeen: minutesAgo(1),
            },
            {
              type: "Normal",
              reason: "PlanPublished",
              message: "Plan billing-p-9",
              count: 1,
              lastSeen: minutesAgo(9),
            },
          ],
        },
      },
    })
    const tabs = await screen.findByRole("tablist", { name: "Sections of billing" })

    await user.click(within(tabs).getByRole("tab", { name: "Events" }))

    expect(history.location.search).toContain("tab=events")
    const events = await screen.findByRole("list", { name: "Events" })
    expect(within(events).getAllByRole("listitem")).toHaveLength(2)
    await user.click(screen.getByRole("button", { name: "Warning 1" }))
    expect(
      within(screen.getByRole("list", { name: "Events" })).getByRole("listitem")
    ).toHaveTextContent("RunFailed ×2")

    await user.click(within(tabs).getByRole("tab", { name: "Related" }))
    expect(screen.getByRole("region", { name: "Related resources" })).toBeVisible()

    await user.click(within(tabs).getByRole("tab", { name: "History" }))
    expect(history.location.search).not.toContain("tab=")
  })

  it("says the Events are not granted rather than showing none", async () => {
    renderConsole("/console/migrations/application/billing?tab=events", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
      diagnostics: {
        events: {
          evidence: Evidence.NOT_GRANTED,
          problem: 'cannot list resource "events"',
        },
      },
    })

    expect(
      await screen.findByText('Events: not granted (cannot list resource "events")')
    ).toBeVisible()
    expect(screen.queryByRole("list", { name: "Events" })).not.toBeInTheDocument()
  })

  it("opens the configuration panel an address names", async () => {
    renderConsole("/console/migrations/application/billing?tab=configuration", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })

    expect(await screen.findByRole("tab", { name: "Configuration" })).toHaveAttribute(
      "aria-selected",
      "true"
    )
    expect(screen.getByRole("table", { name: "Conditions" })).toBeVisible()
    expect(screen.queryByRole("region", { name: "Migration timeline" })).not.toBeInTheDocument()
  })

  it("opens History for a tab the page does not have", async () => {
    const { history } = renderConsole("/console/migrations/application/billing?tab=secrets", {
      migrations: [longHistory()],
      migrationPlan: longPlan,
    })

    expect(await screen.findByRole("region", { name: "Migration timeline" })).toBeVisible()
    expect(history.location.search).not.toContain("secrets")
  })
})
