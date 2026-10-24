import { create } from "@bufbuild/protobuf"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { Evidence } from "../../gen/ptah/console/v1/diagnostics_pb"
import { JournalSchema } from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, syncedMigration } from "../../test/console"

const failed = syncedMigration((m) => {
  m.ref!.name = "journal"
  m.ref!.uid = "uid-journal"
  m.lastRun = {
    $typeName: "ptah.console.v1.MigrationRun",
    outcome: "Failed",
    jobName: "journal-apply-1",
    jobUid: "uid-job",
    startedAt: minutesAgo(6),
    finishedAt: minutesAgo(5),
    appliedVersions: [],
    message: "Migration 2 (Read Ledger) failed and committed nothing",
  }
})

const diagnostics = {
  events: {
    evidence: Evidence.SHOWN,
    events: [
      {
        type: "Warning",
        reason: "MigrationRunFinished",
        message: "Migration 2 failed",
        count: 1,
        lastSeen: minutesAgo(5),
      },
    ],
  },
  jobs: [
    {
      namedBy: "status.lastRun",
      name: "journal-apply-1",
      uid: "uid-job",
      evidence: Evidence.SHOWN,
      state: { failed: 1, startedAt: minutesAgo(6) },
      podsEvidence: Evidence.SHOWN,
      pods: [
        {
          name: "journal-apply-1-x",
          containers: [
            {
              name: "ptah",
              state: "terminated",
              reason: "Error",
              exitCode: 2,
              log: {
                evidence: Evidence.SHOWN,
                text: "\u001b[31mptah-runner: ptah exited with code 2\u001b[0m <script>alert(1)</script>",
              },
            },
          ],
        },
      ],
    },
    { namedBy: "status.unresolvedRun", name: "journal-apply-0", evidence: Evidence.NOT_RETAINED },
  ],
}

function laneOf(text: string) {
  const table = screen.getByRole("table", { name: "Execution and history of journal" })
  const cell = within(table).getByText(text).closest("[data-lane]")
  return cell?.getAttribute("data-lane")
}

describe("Execution and history", () => {
  it("puts the controller, the Job and the database on their own lanes, and pins the original error", async () => {
    const { container } = renderConsole("/console/migrations/application/journal/history", {
      migrations: [failed],
      diagnostics,
    })

    expect(await screen.findByText("journal · MigrationRunFinished")).toBeInTheDocument()
    expect(laneOf("journal · MigrationRunFinished")).toBe("controller")
    expect(laneOf("Job journal-apply-1 started")).toBe("job")
    expect(laneOf("Run ended Failed")).toBe("database")
    const pinned = screen.getByRole("region", { name: "Original error" })
    expect(pinned).toHaveTextContent("The last run ended Failed")
    expect(pinned).toHaveTextContent("Migration 2 (Read Ledger) failed and committed nothing")
    expect(await axe(container)).toHaveNoViolations()
  })

  it("marks a Job Kubernetes no longer keeps instead of leaving it out", async () => {
    renderConsole("/console/migrations/application/journal/history", {
      migrations: [failed],
      diagnostics,
    })

    const gone = await screen.findByText("Job journal-apply-0")
    expect(gone.closest("[data-lane]")).toHaveTextContent("not retained")
  })

  it("shows a log as text, never as markup or terminal colors", async () => {
    const user = userEvent.setup()
    renderConsole("/console/migrations/application/journal/history", {
      migrations: [failed],
      diagnostics,
    })

    await user.click(await screen.findByText("Log excerpt · plain text"))

    const log = screen.getByLabelText("Log of Container ptah terminated")
    expect(log.textContent).toContain("<script>alert(1)</script>")
    expect(log.querySelector("script")).toBeNull()
  })

  it("opens from the resource's page", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console/migrations/application/journal", {
      migrations: [failed],
      diagnostics,
    })

    await user.click(await screen.findByRole("link", { name: "Execution and history" }))

    expect(
      await screen.findByRole("heading", { name: /Execution and history of journal/ })
    ).toBeInTheDocument()
    expect(history.location.pathname).toBe("/console/migrations/application/journal/history")
  })
})

describe("History, with the operator's journal", () => {
  it("lays the journal on the lanes and says from when it is complete", async () => {
    const journaled = syncedMigration((m) => {
      m.journal = create(JournalSchema, {
        coveredSince: minutesAgo(600),
        entries: [
          { type: "PlanPublished", at: minutesAgo(20), plan: "billing-p-1", statements: 2 },
          {
            type: "RunFinished",
            at: minutesAgo(8),
            startedAt: minutesAgo(9),
            outcome: "Succeeded",
            appliedVersions: 2,
          },
        ],
      })
    })
    renderConsole("/console/migrations/application/billing/history", { migrations: [journaled] })

    const table = await screen.findByRole("table", { name: /Execution and history of billing/ })
    expect(within(table).getByText("Plan published")).toBeVisible()
    expect(within(table).getByText("Run applied 2 versions")).toBeVisible()
    expect(screen.getByText(/The operator's journal of billing is complete since/)).toBeVisible()
  })

  it("says no journal is kept where none is", async () => {
    renderConsole("/console/migrations/application/billing/history", {
      migrations: [syncedMigration()],
    })

    expect(await screen.findByText(/The operator keeps no journal of billing/)).toBeVisible()
  })
})
