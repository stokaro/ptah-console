import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { Evidence, GetDiagnosticsResponseSchema } from "../gen/ptah/console/v1/diagnostics_pb"
import {
  ConditionSchema,
  JournalSchema,
  MigrationRunSchema,
} from "../gen/ptah/console/v1/resources_pb"
import { minutesAgo, syncedMigration, waitingSchema } from "../test/console"
import { originalError, timeline } from "./execution"
import { fromMigration, fromSchema } from "./resource"

const inMinutes = (minutes: number) => timestampFromDate(new Date(Date.now() + minutes * 60_000))

const diagnostics = create(GetDiagnosticsResponseSchema, {
  events: {
    evidence: Evidence.SHOWN,
    events: [
      {
        type: "Normal",
        reason: "PlanPublished",
        message: "plan stored",
        count: 1,
        lastSeen: minutesAgo(11),
      },
    ],
  },
  jobs: [
    {
      namedBy: "status.lastRun",
      name: "journal-apply-1",
      evidence: Evidence.SHOWN,
      state: {
        failed: 1,
        startedAt: minutesAgo(6),
        conditions: [
          {
            type: "Failed",
            status: "True",
            reason: "BackoffLimitExceeded",
            lastTransition: minutesAgo(5),
          },
        ],
      },
      events: { evidence: Evidence.NOT_GRANTED, problem: 'cannot list resource "events"' },
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
              log: { evidence: Evidence.SHOWN, text: "ptah-runner: ptah exited with code 2" },
            },
            { name: "fetch-artifact", init: true, state: "terminated", exitCode: 0 },
          ],
        },
      ],
    },
    { namedBy: "status.unresolvedRun", name: "journal-apply-0", evidence: Evidence.NOT_RETAINED },
  ],
})

describe("timeline", () => {
  const schema = fromSchema(
    waitingSchema((s) => {
      s.nextReconciliation = inMinutes(5)
    })
  )

  it("puts the evidence oldest first, a time still due after it, then the undated, and what is gone last", () => {
    const entries = timeline(schema, diagnostics)

    const dated = entries.filter((e) => e.at)
    expect(entries.slice(0, dated.length)).toEqual(dated)
    expect(dated.map((e) => e.at!.getTime())).toEqual(
      dated.map((e) => e.at!.getTime()).sort((a, b) => a - b)
    )
    expect(dated.at(-1)).toMatchObject({ title: "Next reconciliation", due: true })
    const rest = entries.slice(dated.length)
    expect(rest.map((e) => Boolean(e.gone))).toEqual([false, false, true, true])
  })

  it("keeps a Job Kubernetes no longer has as an entry that says so", () => {
    const gone = timeline(schema, diagnostics).find((e) => e.title === "Job journal-apply-0")
    expect(gone).toMatchObject({
      lane: "job",
      gone: "not retained",
      detail: "status.unresolvedRun names it.",
    })
  })

  it("says Events the identity may not read are not granted, rather than leaving them out", () => {
    expect(
      timeline(schema, diagnostics).find((e) => e.title === "Events about Job journal-apply-1")
    ).toMatchObject({
      gone: "not granted",
      detail: 'cannot list resource "events"',
    })
  })

  it("marks a container that failed, with its exit code and log, and one that did not", () => {
    const entries = timeline(schema, diagnostics)
    expect(entries.find((e) => e.title === "Container ptah terminated")).toMatchObject({
      tone: "bad",
      detail: "Error · exit code 2",
      log: "ptah-runner: ptah exited with code 2",
    })
    expect(
      entries.find((e) => e.title === "Container fetch-artifact (init) terminated")
    ).toMatchObject({
      tone: "ok",
    })
  })

  it("puts what the status says about the database on the database lane", () => {
    const migration = fromMigration(
      syncedMigration((m) => {
        m.lastRun = {
          $typeName: "ptah.console.v1.MigrationRun",
          outcome: "Applied",
          jobName: "billing-apply-3",
          jobUid: "",
          startedAt: minutesAgo(30),
          finishedAt: minutesAgo(29),
          appliedVersions: [3n, 4n],
          message: "",
        }
      })
    )
    expect(
      timeline(migration)
        .filter((e) => e.lane === "database")
        .map((e) => [e.title, e.detail])
    ).toEqual([["Run ended Applied", "applied 3, 4"]])
  })
})

describe("originalError", () => {
  it("is a failed run's own account for a migration", () => {
    const migration = fromMigration(
      syncedMigration((m) => {
        m.lastRun = {
          $typeName: "ptah.console.v1.MigrationRun",
          outcome: "Failed",
          jobName: "",
          jobUid: "",
          appliedVersions: [],
          message: "Migration 2 (Read Ledger) failed and committed nothing",
        }
      })
    )
    expect(originalError(migration)).toMatchObject({
      title: "The last run ended Failed",
      text: "Migration 2 (Read Ledger) failed and committed nothing",
    })
  })

  it("is the failure condition, and failing that a failed container", () => {
    const failing = fromSchema(
      waitingSchema((s) => {
        s.conditions.push(
          create(ConditionSchema, {
            type: "ReconciliationFailed",
            status: "True",
            reason: "ApplyFailed",
            message: "the Apply Job failed",
            lastTransition: minutesAgo(5),
          })
        )
      })
    )
    expect(originalError(failing)).toMatchObject({ title: "ReconciliationFailed · ApplyFailed" })
    expect(originalError(fromSchema(waitingSchema()), diagnostics)).toMatchObject({
      title: "Container ptah of Job journal-apply-1 exited 2",
    })
  })

  it("is none when nothing failed", () => {
    const applied = fromMigration(
      syncedMigration((m) => {
        m.lastRun = {
          $typeName: "ptah.console.v1.MigrationRun",
          outcome: "Applied",
          jobName: "",
          jobUid: "",
          appliedVersions: [4n],
          message: "applied 4",
        }
      })
    )
    expect(originalError(applied)).toBeUndefined()
    expect(originalError(fromSchema(waitingSchema()))).toBeUndefined()
  })
})

describe("the journal on the timeline", () => {
  // Both ends of the run from one reading of the clock, so the run takes
  // exactly the minute between them.
  const anchor = Date.now()
  const before = (minutes: number) => timestampFromDate(new Date(anchor - minutes * 60_000))
  const started = before(9)
  const finished = before(8)
  const journaled = syncedMigration((m) => {
    m.lastRun = create(MigrationRunSchema, {
      outcome: "Failed",
      startedAt: started,
      finishedAt: finished,
      message: "Migration 2 failed and committed nothing",
      failure: { version: 2n, statement: 1, statements: 3, sqlState: "23505" },
    })
    m.journal = create(JournalSchema, {
      coveredSince: minutesAgo(600),
      entries: [
        { type: "PlanPublished", at: minutesAgo(20), plan: "billing-p-1", statements: 2 },
        {
          type: "RunFinished",
          at: finished,
          startedAt: started,
          outcome: "Failed",
          failure: { version: 2n, statement: 1, statements: 3, sqlState: "23505" },
        },
      ],
    })
  })

  it("puts each event on its lane, with how long a run took", () => {
    const entries = timeline(fromMigration(journaled))

    expect(entries.find((e) => e.title === "Plan published")?.lane).toBe("controller")
    const run = entries.find((e) => e.title.startsWith("Run failed at migration 2"))
    expect(run).toMatchObject({ lane: "database", tone: "bad" })
    expect(run?.detail).toContain("took 1 min")
  })

  it("lists a run the journal records once, not again from the last run", () => {
    const titles = timeline(fromMigration(journaled)).map((e) => e.title)

    expect(titles.filter((title) => title.startsWith("Run ended"))).toEqual([])
    expect(titles.filter((title) => title.startsWith("Run failed"))).toHaveLength(1)
  })

  it("keeps the last run where the journal does not record it", () => {
    const unjournaled = syncedMigration((m) => {
      m.lastRun = journaled.lastRun
    })

    expect(timeline(fromMigration(unjournaled)).map((e) => e.title)).toContain("Run ended Failed")
  })

  it("pins where the run stopped beside its own account", () => {
    expect(originalError(fromMigration(journaled))?.text).toBe(
      "Migration 2 failed and committed nothing. Stopped at migration 2, statement 1 of 3, SQLSTATE 23505."
    )
  })
})
