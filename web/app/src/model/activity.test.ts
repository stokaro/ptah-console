import { create, type MessageInitShape } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { DecisionKind, DecisionSchema, DecisionState } from "../gen/ptah/console/v1/decisions_pb"
import {
  MigrationSchema,
  SchemaSchema,
  type JournalEntrySchema,
} from "../gen/ptah/console/v1/resources_pb"
import { activity, coverage, deciders, filterActivity } from "./activity"
import { fromMigration, fromSchema } from "./resource"

const start = new Date("2026-10-24T07:00:00Z")
const at = (minutes: number) => timestampFromDate(new Date(start.getTime() + minutes * 60_000))
type Entry = MessageInitShape<typeof JournalEntrySchema>

function schema(name: string, entries: Entry[], coveredSince = 0) {
  return fromSchema(
    create(SchemaSchema, {
      ref: { namespace: "application", name, uid: `uid-${name}` },
      journal: { coveredSince: at(coveredSince), entries },
    })
  )
}

function migration(name: string, entries: Entry[]) {
  return fromMigration(
    create(MigrationSchema, {
      ref: { namespace: "application", name, uid: `uid-${name}` },
      journal: { coveredSince: at(0), entries },
    })
  )
}

function approval(name: string, by: string, minutes: number) {
  return create(DecisionSchema, {
    kind: DecisionKind.SCHEMA_APPROVAL,
    ref: { namespace: "application", name },
    ownerName: "storefront",
    planName: "ptah-plan-a",
    decidedBy: by,
    decidedAt: at(minutes),
    state: DecisionState.CONSUMED,
  })
}

describe("The activity of a schema's journal", () => {
  const storefront = schema("storefront", [
    { type: "PlanPublished", at: at(1), plan: "ptah-plan-a", statements: 3, destructive: true },
    {
      type: "ApprovalAccepted",
      at: at(3),
      plan: "ptah-plan-a",
      approval: "approve-a",
      approver: "alice",
    },
    {
      type: "ApplyFinished",
      at: at(5),
      startedAt: at(4),
      plan: "ptah-plan-a",
      outcome: "Succeeded",
    },
    { type: "Verified", at: at(6), plan: "ptah-plan-a", outcome: "InSync" },
  ])

  it("reads each event as it happened, newest first", () => {
    const entries = activity([storefront], [])

    expect(entries.map((entry) => [entry.type, entry.title, entry.tone])).toEqual([
      ["verification", "Verified in sync", "ok"],
      ["apply", "Apply succeeded", "ok"],
      ["approval", "Approval by alice accepted", "ok"],
      ["plan", "Destructive plan published", "warn"],
    ])
    expect(entries[1].durationMs).toBe(60_000)
    expect(entries[3].details).toEqual(["Plan ptah-plan-a", "3 statements"])
  })

  it("lets a decision the console reads stand for the acceptance it led to", () => {
    const entries = activity([storefront], [approval("approve-a", "alice", 2)])

    const approvals = entries.filter((entry) => entry.type === "approval")
    expect(approvals).toHaveLength(1)
    expect(approvals[0]).toMatchObject({
      title: "Plan approved by alice",
      who: "alice",
      tone: "ok",
    })
    expect(approvals[0].at).toEqual(new Date(start.getTime() + 2 * 60_000))
  })

  it("keeps an acceptance whose decision the console does not read", () => {
    const entries = activity([storefront], [approval("approve-other", "bob", 2)])

    expect(
      entries.filter((entry) => entry.type === "approval").map((entry) => entry.title)
    ).toEqual(["Approval by alice accepted", "Plan approved by bob"])
  })

  it("says an Apply of unknown outcome is settled by reading, never by running it again", () => {
    const [entry] = activity(
      [
        schema("orders", [
          { type: "ApplyFinished", at: at(5), startedAt: at(4), outcome: "Unknown" },
        ]),
      ],
      []
    )

    expect(entry).toMatchObject({ title: "Apply outcome unknown", tone: "unk" })
    expect(entry.details.join(" ")).toContain("never run again blindly")
  })
})

describe("The activity of a migration's journal", () => {
  it("names where a run stopped, and never the engine's message", () => {
    const [entry] = activity(
      [
        migration("billing", [
          {
            type: "RunFinished",
            at: at(9),
            startedAt: at(8),
            outcome: "Partial",
            appliedVersions: 1,
            failure: { version: 2n, statement: 1, statements: 3, sqlState: "23505" },
          },
        ]),
      ],
      []
    )

    expect(entry.title).toBe(
      "Run stopped part-way, at migration 2, statement 1 of 3, SQLSTATE 23505"
    )
    expect(entry.tone).toBe("bad")
    expect(entry.details).toEqual([
      "1 version applied",
      "Stopped at migration 2, statement 1 of 3, SQLSTATE 23505",
    ])
  })

  it("dates attempts that failed alike in a row by the last of them", () => {
    const [entry] = activity(
      [
        migration("billing", [
          {
            type: "OperationFailed",
            at: at(10),
            lastAt: at(30),
            count: 3,
            operation: "History",
            reason: "DatabaseUnreachable",
          },
        ]),
      ],
      []
    )

    expect(entry.title).toBe("History failed: DatabaseUnreachable, 3 times in a row")
    expect(entry.at).toEqual(new Date(start.getTime() + 30 * 60_000))
  })

  it("reads a run applied, one with nothing to do and one nobody can account for", () => {
    const entries = activity(
      [
        migration("billing", [
          { type: "RunFinished", at: at(1), outcome: "Succeeded", appliedVersions: 2 },
          { type: "RunFinished", at: at(2), outcome: "UpToDate" },
          { type: "RunFinished", at: at(3), outcome: "Unknown" },
        ]),
      ],
      []
    )

    expect(entries.map((entry) => [entry.title, entry.tone])).toEqual([
      ["Run outcome unknown", "unk"],
      ["Run found nothing to apply", "ok"],
      ["Run applied 2 versions", "ok"],
    ])
  })
})

describe("What the journals cover", () => {
  it("is complete from the latest coveredSince, and reaches back to the earliest", () => {
    const kept = coverage([schema("a", [], 10), schema("b", [], 40)])

    expect(kept).toEqual({
      complete: new Date(start.getTime() + 40 * 60_000),
      reaches: new Date(start.getTime() + 10 * 60_000),
      withoutJournal: 0,
    })
  })

  it("counts the resources whose operator keeps no journal apart", () => {
    const old = fromSchema(create(SchemaSchema, { ref: { namespace: "application", name: "old" } }))

    expect(coverage([old, schema("a", [], 10)]).withoutJournal).toBe(1)
    expect(coverage([old]).complete).toBeUndefined()
  })
})

describe("Filtering the activity", () => {
  const now = new Date(start.getTime() + 2 * 86_400_000)
  const entries = activity(
    [
      schema("storefront", [
        { type: "PlanPublished", at: at(0), plan: "ptah-plan-a", statements: 1 },
        { type: "OperationFailed", at: at(47 * 60 + 30), operation: "Observe", reason: "Failed" },
      ]),
    ],
    [approval("approve-a", "alice", 47 * 60 + 50)]
  )

  it("keeps one type", () => {
    expect(filterActivity(entries, { type: "failure" }, now).map((entry) => entry.type)).toEqual([
      "failure",
    ])
  })

  it("keeps a period counted back from now", () => {
    expect(filterActivity(entries, { period: "hour" }, now)).toHaveLength(2)
    expect(filterActivity(entries, { period: "week" }, now)).toHaveLength(3)
    expect(filterActivity(entries, { period: "all" }, now)).toHaveLength(3)
  })

  it("keeps one person's decisions", () => {
    expect(filterActivity(entries, { who: "alice" }, now).map((entry) => entry.title)).toEqual([
      "Plan approved by alice",
    ])
    expect(filterActivity(entries, { who: "bob" }, now)).toEqual([])
  })

  it("names everyone who decided, once each", () => {
    expect(deciders([...entries, ...entries])).toEqual(["alice"])
  })
})
