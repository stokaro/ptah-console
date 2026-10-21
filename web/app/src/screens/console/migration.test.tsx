import { create } from "@bufbuild/protobuf"
import { Code, ConnectError } from "@connectrpc/connect"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  AttentionKind,
  AttentionSchema,
  MigrationPlanSchema,
  StageState,
} from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, syncedMigration } from "../../test/console"

const failed = syncedMigration((m) => {
  m.ref!.name = "journal"
  m.ref!.uid = "uid-journal"
  m.phase = "Failed"
  m.planName = "journal-p-1"
  m.planUid = "uid-journal-plan"
  m.stages[4].state = StageState.FAILED
  m.attention = [
    create(AttentionSchema, {
      kind: AttentionKind.FAILED,
      reason: "Migration 2 (Read Ledger) failed and committed nothing.",
      nextStep: "Fix the migration in the artifact, or the database, and the next read continues.",
      since: minutesAgo(3),
    }),
  ]
  m.history = {
    $typeName: "ptah.console.v1.MigrationHistory",
    observedAt: minutesAgo(2),
    currentVersion: 1n,
    checkpointVersion: 0n,
    appliedCount: 1,
    pendingCount: 2,
    dirty: true,
    modifiedVersions: [],
    outOfOrderVersions: [],
  }
  m.lastRun = {
    $typeName: "ptah.console.v1.MigrationRun",
    outcome: "Failed",
    jobName: "journal-apply-1",
    jobUid: "uid-job",
    startedAt: minutesAgo(4),
    finishedAt: minutesAgo(3),
    appliedVersions: [1n],
    message:
      "Migration 2 (Read Ledger) failed and committed nothing; 1 migrations before it are recorded applied",
  }
})

const journalPlan = create(MigrationPlanSchema, {
  namespace: "application",
  migration: "journal",
  name: "journal-p-1",
  uid: "uid-journal-plan",
  migrations: [
    { version: 2n, versionKey: "2", description: "read ledger", checksum: "h1:aa" },
    { version: 3n, versionKey: "3", description: "add totals", checksum: "h1:bb" },
  ],
})

describe("Migration detail", () => {
  it("shows how far the history is, how the last run ended, and what would run next", async () => {
    const { container } = renderConsole("/console/migrations/application/journal", {
      migrations: [failed],
      migrationPlan: journalPlan,
    })

    expect(
      await screen.findByRole("heading", { name: /PtahMigration \/ journal/ })
    ).toBeInTheDocument()
    const run = screen.getByRole("region", { name: "Last run" })
    expect(run).toHaveTextContent("Failed")
    expect(run).toHaveTextContent("Migration 2 (Read Ledger) failed and committed nothing")
    expect(screen.getByRole("region", { name: "History" })).toHaveTextContent(
      "A failed or interrupted run left a row"
    )
    const steps = await screen.findByRole("table", { name: "Steps of journal" })
    expect(
      within(steps)
        .getAllByRole("row")
        .slice(1)
        .map((row) => row.textContent)
    ).toEqual([
      "1Applied by the last run✓applied",
      "2read ledger–held: the history is dirty",
      "3add totals–held: the history is dirty",
    ])
    expect(screen.getByRole("region", { name: "Supported actions" })).toHaveTextContent(
      "Retry, rollback and force are not offered"
    )
    expect(await axe(container)).toHaveNoViolations()
  })

  it("offers to acknowledge an unresolved run, and only where it may", async () => {
    const user = userEvent.setup()
    const unresolved = syncedMigration((m) => {
      m.unresolvedRun = {
        $typeName: "ptah.console.v1.UnresolvedRun",
        outcome: "Unknown",
        operationId: "op-2",
        jobName: "billing-run-2",
        jobUid: "uid-job",
        planName: "",
        planUid: "",
        recordedAt: minutesAgo(5),
      }
    })
    renderConsole("/console/migrations/application/billing", { migrations: [unresolved] })

    await user.click(await screen.findByRole("button", { name: "Acknowledge this run…" }))

    expect(await screen.findByRole("alertdialog")).toHaveTextContent("Acknowledge run op-2?")
  })

  it("offers no acknowledgment in a read-only console, and says why", async () => {
    const unresolved = syncedMigration((m) => {
      m.unresolvedRun = {
        $typeName: "ptah.console.v1.UnresolvedRun",
        outcome: "Unknown",
        operationId: "op-2",
        jobName: "billing-run-2",
        jobUid: "uid-job",
        planName: "",
        planUid: "",
        recordedAt: minutesAgo(5),
      }
    })
    renderConsole("/console/migrations/application/billing", {
      migrations: [unresolved],
      readOnly: true,
    })

    expect(
      await screen.findByText("This console runs read-only: it sends no decision.")
    ).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Acknowledge this run…" })).toBeNull()
  })

  it("says why the plan is not shown", async () => {
    renderConsole("/console/migrations/application/journal", {
      migrations: [failed],
      migrationPlan: new ConnectError(
        'cannot get resource "ptahmigrationplans"',
        Code.PermissionDenied
      ),
    })

    expect(
      await screen.findByText(/The plan is not shown: cannot get resource "ptahmigrationplans"/)
    ).toBeInTheDocument()
  })

  it("says nothing is pending when the history is current", async () => {
    renderConsole("/console/migrations/application/billing", {
      migrations: [
        syncedMigration((m) => {
          m.history = {
            $typeName: "ptah.console.v1.MigrationHistory",
            observedAt: minutesAgo(2),
            currentVersion: 4n,
            checkpointVersion: 0n,
            appliedCount: 4,
            pendingCount: 0,
            dirty: false,
            modifiedVersions: [],
            outOfOrderVersions: [],
          }
        }),
      ],
    })

    expect(
      await screen.findByText("Nothing pending: the history stands at version 4.")
    ).toBeInTheDocument()
  })

  it("opens from the resource list", async () => {
    const user = userEvent.setup()
    renderConsole("/console/resources", { migrations: [failed], migrationPlan: journalPlan })

    await user.click(await screen.findByRole("link", { name: /journal/ }))

    expect(
      await screen.findByRole("heading", { name: /PtahMigration \/ journal/ })
    ).toBeInTheDocument()
  })
})
