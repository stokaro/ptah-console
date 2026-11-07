import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { DecisionSchema, DecisionState } from "../gen/ptah/console/v1/decisions_pb"
import { MigrationPlanSchema, SchemaSchema, SqlAccess } from "../gen/ptah/console/v1/resources_pb"
import { storedPlan } from "../test/console"
import {
  approvalChecks,
  filterStatements,
  integrity,
  planReadCheck,
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
  const base = {
    current: true,
    open: true,
    allowed: "allowed" as const,
    namespace: "application",
    planRead: planReadCheck({ plan: storedPlan(), failed: false }),
  }

  it("all pass for a current plan an approver may approve", () => {
    expect(approvalChecks({ ...base, contractSupported: true }).map((check) => check.tone)).toEqual(
      ["ok", "ok", "ok", "ok", "ok"]
    )
  })

  it("leave the contract out for a migration, whose SQL the console does not read", () => {
    const planRead = planReadCheck({ plan: create(MigrationPlanSchema), failed: false })
    expect(approvalChecks({ ...base, planRead }).map((check) => check.label)).toEqual([
      "The plan you read is the current plan",
      "The operator waits for the decision",
      "The console read this plan's steps",
      "This identity may create approvals in application",
    ])
  })

  it("leave the read out while there is no plan to read", () => {
    expect(approvalChecks({ ...base, planRead: undefined }).map((check) => check.label)).toEqual([
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

describe("The check on the plan read", () => {
  const read = (access: SqlAccess) =>
    planReadCheck({ plan: storedPlan((plan) => (plan.sqlAccess = access)), failed: false })

  it.each([
    [SqlAccess.UNSPECIFIED, "bad", "Whether this plan can be read is not known"],
    [SqlAccess.NOT_GRANTED, "bad", "This identity may not read this plan's SQL"],
    [SqlAccess.INTEGRITY_FAILED, "bad", "The stored plan failed verification"],
    [SqlAccess.UNSUPPORTED, "bad", "This console does not read the form this plan is written in"],
    [SqlAccess.AVAILABLE, "ok", "The console read and verified this plan"],
  ])("answers SQL access %s", (access, tone, label) => {
    expect(read(access)).toMatchObject({ tone, label })
  })

  it("fails on a read that failed, even with the plan an earlier read returned", () => {
    expect(planReadCheck({ plan: storedPlan(), failed: true })).toMatchObject({
      tone: "bad",
      label: "The console could not read this plan",
    })
    expect(planReadCheck({ failed: true }).tone).toBe("bad")
  })

  it("waits for a read that is running, and offers nothing meanwhile", () => {
    expect(planReadCheck({ failed: false }).tone).toBe("run")
  })

  it("passes a migration plan, which has no SQL to verify, once it read", () => {
    expect(planReadCheck({ plan: create(MigrationPlanSchema), failed: false }).tone).toBe("ok")
  })
})
