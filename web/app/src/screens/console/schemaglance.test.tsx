import { create } from "@bufbuild/protobuf"
import { screen, within } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  JournalSchema,
  PendingObservationSchema,
  StageState,
  TargetSchema,
} from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, waitingSchema } from "../../test/console"

function tiles() {
  return within(screen.getByRole("list", { name: "At a glance" }))
    .getAllByRole("listitem")
    .map((tile) => tile.textContent)
}

describe("A schema's page at a glance", () => {
  it("puts the plan, the drift, the last Apply and the next read first", async () => {
    const { container } = renderConsole("/console/schemas/application/storefront", {
      schemas: [
        waitingSchema((s) => {
          s.target = create(TargetSchema, {
            engine: "PostgreSQL",
            lastObservedAt: minutesAgo(5),
            highestDriftSeverity: "destructive",
            driftFindingCount: 2,
          })
          s.nextReconciliation = minutesAgo(-20)
        }),
      ],
    })

    await screen.findByRole("list", { name: "At a glance" })
    expect(tiles()).toEqual([
      "!Current planWaiting3 statements, destructive",
      "×Driftdestructive2 findings at the read 5 min ago",
      "–AppliedNeverNo plan has reached the database",
      expect.stringMatching(/^–Next readin 20 min/),
    ])
    expect(await axe(container)).toHaveNoViolations()
  })

  it("says the database is not read yet rather than that nothing drifted", async () => {
    renderConsole("/console/schemas/application/storefront", { schemas: [waitingSchema()] })

    await screen.findByRole("list", { name: "At a glance" })
    expect(tiles()[1]).toBe("–Drift–The database is not read yet")
  })
})

describe("A schema managed by GitOps", () => {
  it("says the controller sets a spec change back", async () => {
    renderConsole("/console/schemas/application/storefront", {
      schemas: [
        waitingSchema((s) => {
          s.gitops = {
            $typeName: "ptah.console.v1.GitOps",
            controller: "Argo CD",
            application: "storefront",
          }
        }),
      ],
    })

    expect(
      await screen.findByText(
        /Managed by Argo CD · storefront\. A change to its spec made here is set back/
      )
    ).toBeVisible()
  })

  it("says nothing of GitOps for a schema nothing else manages", async () => {
    renderConsole("/console/schemas/application/storefront", { schemas: [waitingSchema()] })

    await screen.findByRole("list", { name: "At a glance" })
    expect(screen.queryByText(/Managed by/)).not.toBeInTheDocument()
  })
})

describe("A schema's observations and verification", () => {
  it("lists what the journal says the operator saw, newest first, and links the history", async () => {
    renderConsole("/console/schemas/application/storefront", {
      schemas: [
        waitingSchema((s) => {
          s.stages[4].state = StageState.DONE
          s.journal = create(JournalSchema, {
            coveredSince: minutesAgo(600),
            entries: [
              { type: "Verified", at: minutesAgo(90), outcome: "InSync", plan: "storefront-p-1" },
              {
                type: "OperationFailed",
                at: minutesAgo(30),
                lastAt: minutesAgo(20),
                operation: "Observe",
                reason: "TargetUnavailable",
                count: 2,
              },
              {
                type: "Verified",
                at: minutesAgo(10),
                outcome: "DriftRemains",
                plan: "storefront-p-2",
              },
            ],
          })
        }),
      ],
    })

    const observations = await screen.findByRole("region", { name: "Recent observations" })
    expect(
      within(observations)
        .getAllByRole("listitem")
        .map((item) => item.textContent)
    ).toEqual([
      "!Drift remains after the Apply10 min ago",
      "×Observe failed: TargetUnavailable, 2 times in a row20 min ago",
      "✓Verified in sync2 h ago",
    ])
    expect(
      within(observations).getByRole("link", { name: "All of its history →" })
    ).toHaveAttribute("href", "/console/schemas/application/storefront/history")
    const verification = screen.getByRole("region", { name: "Verification" })
    expect(verification).toHaveTextContent("Verified")
    expect(verification).toHaveTextContent(
      "Last in the journal: Drift remains after the Apply, 10 min ago."
    )
  })

  it("says a fresh read is due to settle an Apply, and that verification has not started", async () => {
    renderConsole("/console/schemas/application/storefront", {
      schemas: [
        waitingSchema((s) => {
          s.pendingObservation = create(PendingObservationSchema, {
            outcome: "ApplySucceeded",
            applyJobName: "storefront-apply-8f2c",
            observeAfter: minutesAgo(-3),
          })
        }),
      ],
    })

    const verification = await screen.findByRole("region", { name: "Verification" })
    expect(verification).toHaveTextContent("Not started")
    expect(verification).toHaveTextContent(
      "A fresh read settles the Apply of Job storefront-apply-8f2c (ApplySucceeded), in 3 min."
    )
    expect(verification).toHaveTextContent("The journal records no verification yet.")
  })
})
