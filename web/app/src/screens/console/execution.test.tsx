import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { Evidence } from "../../gen/ptah/console/v1/diagnostics_pb"
import { Answer, StageState } from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, syncedMigration, waitingSchema } from "../../test/console"

const applying = () =>
  waitingSchema((schema) => {
    schema.phase = "Applying"
    schema.attention = []
    schema.stages[2].state = StageState.DONE
    schema.stages[3].state = StageState.ACTIVE
    schema.activeOperation = {
      $typeName: "ptah.console.v1.Operation",
      type: "Apply",
      id: "op-1",
      jobName: "storefront-apply-8f2c",
      jobUid: "uid-job",
      startedAt: minutesAgo(2),
      attempt: 1,
    }
    schema.target = {
      $typeName: "ptah.console.v1.Target",
      engine: "postgres",
      secretName: "storefront-db",
      secretKey: "dsn",
      realm: "",
      lastObservedAt: minutesAgo(30),
      highestDriftSeverity: "",
      driftFindingCount: 0,
      driftFindings: [],
    }
  })

const applyJob = {
  namedBy: "status.activeOperation",
  name: "storefront-apply-8f2c",
  evidence: Evidence.SHOWN,
  state: { active: 1, startedAt: minutesAgo(2) },
  podsEvidence: Evidence.SHOWN,
  pods: [
    {
      name: "storefront-apply-8f2c-x1",
      phase: "Running",
      containers: [
        {
          name: "ptah",
          state: "running",
          log: { evidence: Evidence.SHOWN, text: "applying statement 2 of 3\n" },
        },
      ],
    },
  ],
}

describe("Execution and diagnosis", () => {
  it("follows a running Apply: its Job, its log, what the database was last seen to be", async () => {
    const { container } = renderConsole("/console/schemas/application/storefront/execution", {
      schemas: [applying()],
      migrations: [],
      diagnostics: {
        jobs: [applyJob],
        events: {
          evidence: Evidence.SHOWN,
          events: [
            {
              type: "Normal",
              reason: "ApplyStarted",
              message: "Job storefront-apply-8f2c",
              count: 1,
              lastSeen: minutesAgo(2),
            },
          ],
        },
      },
    })

    expect(
      await screen.findByRole("heading", { level: 1, name: "Execution & diagnosis" })
    ).toBeVisible()
    const summary = screen.getByRole("region", { name: "Summary" })
    expect(summary).toHaveTextContent("Apply running")
    expect(summary).toHaveTextContent("Attempt 1, started 2 min ago")
    const jobs = await screen.findByRole("list", { name: "Jobs the status names" })
    expect(jobs).toHaveTextContent("storefront-apply-8f2c")
    expect(jobs).toHaveTextContent("Running")
    expect(screen.getByText("applying statement 2 of 3")).toBeVisible()
    expect(screen.getByText(/Read 30 min ago: no drift from the desired schema/)).toBeVisible()
    expect(screen.getByText("ApplyStarted")).toBeVisible()
    expect(screen.getByText("key dsn; the console never reads it")).toBeVisible()
    expect(screen.getByText(/No blind retry, no automatic rollback/)).toBeVisible()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("asks for no log an identity may not read", async () => {
    const sent: boolean[] = []
    renderConsole("/console/schemas/application/storefront/execution", {
      schemas: [applying()],
      migrations: [],
      capabilities: {
        listSchemas: Answer.ALLOWED,
        listMigrations: Answer.ALLOWED,
        readJobs: Answer.ALLOWED,
        readPods: Answer.ALLOWED,
        readLogs: Answer.DENIED,
        readEvents: Answer.ALLOWED,
      },
      // As the server does: a log only for a request that asks for one.
      diagnostics: (request) => {
        sent.push(request.includeLogs)
        return request.includeLogs ? { jobs: [applyJob] } : { jobs: [{ ...applyJob, pods: [] }] }
      },
    })

    expect(await screen.findByRole("list", { name: "Jobs the status names" })).toBeVisible()
    expect(sent).toEqual([false])
    expect(screen.queryByText("applying statement 2 of 3")).not.toBeInTheDocument()
  })

  it("says why a Job is not shown rather than leaving it out", async () => {
    renderConsole("/console/schemas/application/storefront/execution", {
      schemas: [applying()],
      migrations: [],
      diagnostics: {
        jobs: [
          {
            namedBy: "status.activeOperation",
            name: "storefront-apply-8f2c",
            evidence: Evidence.NOT_RETAINED,
          },
        ],
      },
    })

    expect(await screen.findByRole("list", { name: "Jobs the status names" })).toHaveTextContent(
      "Job storefront-apply-8f2c: not retained"
    )
  })

  it("counts what failed over the period the journal covers, and says that period", async () => {
    const failing = syncedMigration((migration) => {
      migration.phase = "Failed"
      migration.journal = {
        $typeName: "ptah.console.v1.Journal",
        coveredSince: timestampFromDate(new Date("2026-10-01T00:00:00Z")),
        entries: [
          {
            $typeName: "ptah.console.v1.JournalEntry",
            type: "OperationFailed",
            at: minutesAgo(90),
            lastAt: minutesAgo(60),
            count: 3,
            plan: "",
            statements: 0,
            destructive: false,
            approval: "",
            approver: "",
            outcome: "",
            operation: "History",
            reason: "TargetUnavailable",
            appliedVersions: 0,
            acknowledgment: "",
            acknowledgedBy: "",
          },
        ],
      }
    })
    renderConsole("/console/migrations/application/billing/execution", {
      schemas: [],
      migrations: [failing],
    })

    const causes = await screen.findByText("History: TargetUnavailable")
    expect(causes.parentElement).toHaveTextContent("3")
    expect(
      screen.getByText(/From the operator's journal of this resource, complete since/)
    ).toBeVisible()
  })

  it("says the operator keeps no journal when it does not, and invents no history", async () => {
    renderConsole("/console/migrations/application/billing/execution", {
      schemas: [],
      migrations: [syncedMigration()],
    })

    expect(await screen.findByText("The operator keeps no journal of this resource.")).toBeVisible()
  })

  it("is one link away from the resource's page", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console/schemas/application/storefront", {
      schemas: [applying()],
      migrations: [],
    })

    await user.click(await screen.findByRole("link", { name: "Execution & diagnosis" }))

    expect(history.location.pathname).toBe("/console/schemas/application/storefront/execution")
    const crumbs = await screen.findByRole("navigation", { name: "Breadcrumb" })
    expect(within(crumbs).getByRole("link", { name: "storefront" })).toBeVisible()
  })
})
