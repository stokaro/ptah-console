import { create } from "@bufbuild/protobuf"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  DecisionKind,
  DecisionOutcome,
  DecisionSchema,
  type AcknowledgeMigrationRunRequest,
} from "../gen/ptah/console/v1/decisions_pb"
import { Evidence } from "../gen/ptah/console/v1/diagnostics_pb"
import { AttentionKind, MigrationSchema, StageState } from "../gen/ptah/console/v1/resources_pb"
import { WatchedKind } from "../gen/ptah/console/v1/watch_pb"
import { heartbeat, minutesAgo, renderConsole, result, scriptedWatch } from "../test/console"

const unresolved = create(MigrationSchema, {
  ref: { namespace: "application", name: "billing", uid: "uid-migration", resourceVersion: "7" },
  phase: "Blocked",
  statusCurrent: true,
  stages: [{ name: "Apply", state: StageState.UNKNOWN }],
  attention: [
    {
      kind: AttentionKind.UNRESOLVED_RUN,
      reason: "Run op-2 ended without a result the operator can trust.",
      nextStep: "Read the database's migration history, then acknowledge run op-2.",
    },
  ],
  unresolvedRun: { operationId: "op-2", outcome: "Unknown", jobName: "billing-run-2" },
})

const settledInvalidation = {
  event: {
    case: "invalidate" as const,
    value: { kind: WatchedKind.MIGRATION, namespace: "application", name: "billing" },
  },
}

const diagnostics = {
  events: {
    evidence: Evidence.SHOWN,
    events: [
      {
        type: "Warning",
        reason: "RunUnresolved",
        message: "the runner's Pod was deleted",
        count: 1,
        lastSeen: minutesAgo(3),
      },
    ],
  },
  jobs: [
    {
      namedBy: "status.unresolvedRun",
      name: "billing-run-2",
      uid: "uid-job",
      evidence: Evidence.SHOWN,
      state: { failed: 1, startedAt: minutesAgo(6) },
      events: {
        evidence: Evidence.NOT_GRANTED,
        problem: 'User "oncall" cannot list resource "events"',
      },
      podsEvidence: Evidence.SHOWN,
      pods: [
        {
          name: "billing-run-2-x7k2p",
          phase: "Failed",
          containers: [
            {
              name: "runner",
              state: "terminated",
              exitCode: 137,
              reason: "Error",
              log: { evidence: Evidence.SHOWN, text: "applying 016\nkilled\n", truncated: true },
            },
          ],
        },
      ],
    },
    {
      namedBy: "status.lastRun",
      name: "billing-run-1",
      evidence: Evidence.NOT_RETAINED,
      problem: "Kubernetes no longer keeps the Job.",
    },
  ],
}

describe("On call", () => {
  it("shows what is kept, what is not, and the log as text", async () => {
    const { container } = renderConsole("/incidents/application/migration/billing", {
      migrations: [unresolved],
      diagnostics,
    })

    const kept = await screen.findByRole("list", { name: "Evidence" })
    expect(within(kept).getByText("RunUnresolved")).toBeInTheDocument()
    expect(within(kept).getByText("Job billing-run-1: not retained")).toBeInTheDocument()
    expect(
      within(kept).getByText("Events about Job billing-run-2: not granted")
    ).toBeInTheDocument()
    expect(within(kept).getByText("runner exited 137 (Error)")).toBeInTheDocument()
    expect(screen.getByLabelText("Log excerpt")).toHaveTextContent("applying 016 killed")
    expect(screen.getByText("Earlier lines are not shown.")).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("acknowledges exactly the run that waits", async () => {
    const user = userEvent.setup()
    const sent: AcknowledgeMigrationRunRequest[] = []
    renderConsole("/incidents/application/migration/billing", {
      migrations: [unresolved],
      diagnostics,
      acknowledge: (request) => {
        sent.push(request)
        return result({
          outcome: DecisionOutcome.ADMITTED,
          decision: create(DecisionSchema, {
            kind: DecisionKind.RUN_ACKNOWLEDGMENT,
            decidedBy: "oncall",
          }),
        })
      },
    })

    await user.click(await screen.findByRole("button", { name: "Acknowledge this run…" }))
    const dialog = await screen.findByRole("alertdialog")
    expect(
      within(dialog).getByText(/not evidence that the SQL effect was verified/)
    ).toBeInTheDocument()
    await user.click(within(dialog).getByRole("checkbox"))
    await user.click(within(dialog).getByRole("button", { name: "Confirm" }))

    expect(await within(dialog).findByText("Acknowledgment admitted")).toBeInTheDocument()
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({
      migrationName: "billing",
      migrationUid: "uid-migration",
      operationId: "op-2",
    })
  })

  it("keeps saying how the acknowledgment ended once the operator has settled the run", async () => {
    const user = userEvent.setup()
    const watch = scriptedWatch()
    const settled = create(MigrationSchema, {
      ref: unresolved.ref,
      phase: "Pending",
      statusCurrent: true,
      stages: [{ name: "Apply", state: StageState.WAITING }],
      attention: [
        {
          kind: AttentionKind.WAITING_FOR_APPROVAL,
          reason: "1 planned migration needs an approval naming this plan",
        },
      ],
    })
    let current = unresolved
    renderConsole("/incidents/application/migration/billing", {
      migrations: () => [current],
      diagnostics,
      watch: watch.stream,
      acknowledge: () =>
        result({
          outcome: DecisionOutcome.ADMITTED,
          decision: create(DecisionSchema, {
            kind: DecisionKind.RUN_ACKNOWLEDGMENT,
            decidedBy: "oncall",
          }),
        }),
    })
    watch.push(heartbeat())

    await user.click(await screen.findByRole("button", { name: "Acknowledge this run…" }))
    const dialog = await screen.findByRole("alertdialog")
    await user.click(within(dialog).getByRole("checkbox"))
    await user.click(within(dialog).getByRole("button", { name: "Confirm" }))
    expect(await within(dialog).findByText("Acknowledgment admitted")).toBeInTheDocument()

    // The operator settles the run as soon as the acknowledgment is in, and
    // the page reads it.
    current = settled
    watch.push(settledInvalidation)
    expect(
      await screen.findByRole("heading", { name: "Waiting for approval", hidden: true })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "Acknowledge this run…", hidden: true })
    ).not.toBeInTheDocument()
    expect(
      within(screen.getByRole("alertdialog")).getByText("Acknowledgment admitted")
    ).toBeVisible()
  })

  it("shows the log of the run the incident is about, from the container that ran it", async () => {
    renderConsole("/incidents/application/migration/billing", {
      migrations: [unresolved],
      diagnostics: {
        jobs: [
          {
            namedBy: "status.activeOperation",
            name: "billing-history-3",
            evidence: Evidence.SHOWN,
            podsEvidence: Evidence.SHOWN,
            pods: [
              {
                name: "billing-history-3-q1w2e",
                phase: "Pending",
                containers: [
                  {
                    name: "fetch-migrations",
                    init: true,
                    state: "terminated",
                    log: { evidence: Evidence.SHOWN, text: "Pulled the artifact\n" },
                  },
                ],
              },
            ],
          },
          {
            namedBy: "status.unresolvedRun",
            name: "billing-run-2",
            evidence: Evidence.SHOWN,
            podsEvidence: Evidence.SHOWN,
            pods: [
              {
                name: "billing-run-2-x7k2p",
                phase: "Succeeded",
                containers: [
                  {
                    name: "install-runner",
                    init: true,
                    state: "terminated",
                    reason: "Completed",
                    log: { evidence: Evidence.SHOWN, text: "installed the runner\n" },
                  },
                  {
                    name: "ptah",
                    state: "terminated",
                    reason: "Completed",
                    log: {
                      evidence: Evidence.SHOWN,
                      text: "relation ledger_entries does not exist\n",
                    },
                  },
                ],
              },
            ],
          },
        ],
      },
    })

    expect(await screen.findByLabelText("Log excerpt")).toHaveTextContent(
      "relation ledger_entries does not exist"
    )
    expect(
      screen.getByText("Log excerpt · billing-run-2-x7k2p / ptah · plain text")
    ).toBeInTheDocument()
    const kept = screen.getByRole("list", { name: "Evidence" })
    expect(within(kept).getByText("ptah exited 0 (Completed)")).toBeInTheDocument()
  })

  it("lists what needs a person and says what is not offered", async () => {
    renderConsole("/incidents", { migrations: [unresolved] })

    expect(await screen.findByRole("link", { name: "Unresolved run: billing" })).toBeInTheDocument()
    expect(screen.getByText(/Retry, rollback and force apply are not offered/)).toBeInTheDocument()
  })
})

describe("The next supported step", () => {
  it("offers the operator plugin's command that shows the same state", async () => {
    const withCommand = create(MigrationSchema, unresolved)
    withCommand.attention[0].command = "kubectl ptah migration billing -n application"
    renderConsole("/incidents/application/migration/billing", {
      migrations: [withCommand],
      diagnostics,
    })

    expect(await screen.findByLabelText("Command that shows the same state")).toHaveTextContent(
      "kubectl ptah migration billing -n application"
    )
    expect(screen.getByRole("button", { name: "Copy" })).toBeInTheDocument()
  })
})
