import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { DecisionSchema, DecisionState } from "../gen/ptah/console/v1/decisions_pb"
import { SchemaSchema, SqlAccess } from "../gen/ptah/console/v1/resources_pb"
import { storedPlan } from "../test/console"
import {
  approvalChecks,
  filterStatements,
  integrity,
  statementTypes,
  tombstone,
} from "./planreview"
import { fromSchema } from "./resource"

describe("Statement types", () => {
  it("counts the plan's own marks, the unmarked apart", () => {
    expect(statementTypes(storedPlan().statements)).toEqual({
      marked: [
        { tag: "drops a column", tone: "bad", count: 1 },
        { tag: "grants", tone: "warn", count: 1 },
      ],
      unmarked: 1,
    })
  })
})

describe("Integrity", () => {
  it("is verified when the SQL could be shown, chunk by chunk", () => {
    expect(integrity(storedPlan())).toMatchObject({
      tone: "ok",
      title: "Verified",
      lines: [
        "2 chunks, each checked by size, digest and order before any SQL was shown",
        expect.stringMatching(/^Content /),
      ],
    })
  })

  it.each([
    [SqlAccess.NOT_GRANTED, "off", "Not checked by this identity"],
    [SqlAccess.INTEGRITY_FAILED, "bad", "The stored plan failed a check"],
    [SqlAccess.UNSUPPORTED, "unk", "Written under a contract this console does not read"],
  ])("says what it could not establish (%i)", (access, tone, title) => {
    expect(integrity(storedPlan((plan) => (plan.sqlAccess = access)))).toMatchObject({
      tone,
      title,
    })
  })

  it("never shows part of a plan that failed a check", () => {
    const failed = integrity(storedPlan((plan) => (plan.sqlAccess = SqlAccess.INTEGRITY_FAILED)))
    expect(failed.lines.join(" ")).toContain("a partial plan is never shown")
  })
})

describe("Filtering the statements", () => {
  const statements = storedPlan().statements

  it("keeps the plan's numbering", () => {
    expect(filterStatements(statements, "grant", false).map((s) => s.number)).toEqual([3])
  })

  it("keeps only the marked ones when asked", () => {
    expect(filterStatements(statements, "", true).map((s) => s.number)).toEqual([2, 3])
  })

  it("matches nothing it does not contain", () => {
    expect(filterStatements(statements, "truncate", false)).toEqual([])
  })
})

describe("A plan the link named", () => {
  const schema = fromSchema(
    create(SchemaSchema, {
      ref: { namespace: "application", name: "storefront", uid: "uid-schema" },
      currentPlan: { name: "storefront-p-new", uid: "uid-new" },
      journal: {
        coveredSince: timestampFromDate(new Date("2026-10-20T00:00:00Z")),
        entries: [
          {
            type: "PlanPublished",
            at: timestampFromDate(new Date("2026-10-24T07:00:00Z")),
            plan: "storefront-p-old",
          },
        ],
      },
    })
  )
  const stale = create(DecisionSchema, {
    ref: { namespace: "application", name: "approve-old" },
    ownerName: "storefront",
    planName: "storefront-p-old",
    decidedBy: "alice",
    state: DecisionState.STALE,
  })

  it("is no tombstone when it is the current plan, or when none was named", () => {
    expect(tombstone(schema, "storefront-p-new", [])).toBeUndefined()
    expect(tombstone(schema, undefined, [])).toBeUndefined()
  })

  it("is one when another replaced it, with what the journal and the decisions say of it", () => {
    expect(tombstone(schema, "storefront-p-old", [stale])).toEqual({
      name: "storefront-p-old",
      current: "storefront-p-new",
      published: new Date("2026-10-24T07:00:00Z"),
      applied: undefined,
      approvals: [{ name: "approve-old", by: "alice", state: DecisionState.STALE }],
    })
  })

  it("names no approval of another resource's plan of the same name", () => {
    const other = create(DecisionSchema, { ...stale, ownerName: "inventory" })
    expect(tombstone(schema, "storefront-p-old", [other])?.approvals).toEqual([])
  })
})

describe("The checks before an approval", () => {
  const base = { current: true, open: true, allowed: "allowed" as const, namespace: "application" }

  it("all pass for a current plan an approver may approve", () => {
    expect(approvalChecks({ ...base, contractSupported: true }).map((check) => check.tone)).toEqual(
      ["ok", "ok", "ok", "ok"]
    )
  })

  it("leave the contract out for a migration, whose SQL the console does not read", () => {
    expect(approvalChecks(base).map((check) => check.label)).toEqual([
      "The plan you read is the current plan",
      "The operator waits for the decision",
      "This identity may create approvals in application",
    ])
  })

  it.each([
    [{ current: false }, "The plan you read is no longer current"],
    [{ open: false }, "The operator is reading the resource again"],
    [{ contractSupported: false }, "Its contract is not one this console reads"],
    [{ allowed: "denied" as const }, "This identity may not create approvals in application"],
    [{ allowed: "read-only" as const }, "This console sends no decision"],
    [{ allowed: "offline" as const }, "The cluster is out of reach"],
  ])("fail on %j", (change, label) => {
    const checks = approvalChecks({ ...base, contractSupported: true, ...change })
    expect(checks.find((check) => check.tone !== "ok")?.label).toBe(label)
  })
})
