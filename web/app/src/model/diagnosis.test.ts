import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { MigrationSchema, SchemaSchema } from "../gen/ptah/console/v1/resources_pb"
import { failureCauses, lastOutcome, relatedResources } from "./diagnosis"
import { fromMigration, fromSchema } from "./resource"

const at = (iso: string) => timestampFromDate(new Date(iso))

describe("Failure causes", () => {
  const migration = fromMigration(
    create(MigrationSchema, {
      ref: { namespace: "application", name: "billing", uid: "uid-migration" },
      journal: {
        coveredSince: at("2026-10-01T00:00:00Z"),
        entries: [
          {
            type: "OperationFailed",
            at: at("2026-10-20T10:00:00Z"),
            lastAt: at("2026-10-20T10:30:00Z"),
            count: 4,
            operation: "History",
            reason: "TargetUnavailable",
          },
          {
            type: "RunFinished",
            at: at("2026-10-21T09:00:00Z"),
            outcome: "Failed",
            failure: { version: 16n, statement: 2, statements: 3, sqlState: "23505" },
          },
          {
            type: "RunFinished",
            at: at("2026-10-22T09:00:00Z"),
            outcome: "Partial",
            failure: { version: 17n, sqlState: "23505" },
          },
          {
            type: "OperationFailed",
            at: at("2026-10-23T08:00:00Z"),
            operation: "History",
            reason: "TargetUnavailable",
          },
          { type: "RunFinished", at: at("2026-10-23T09:00:00Z"), outcome: "Succeeded" },
          { type: "RunFinished", at: at("2026-10-23T10:00:00Z"), outcome: "Unknown" },
        ],
      },
    })
  )

  it("counts each cause as often as the journal folded it, the most frequent first", () => {
    expect(failureCauses(migration)).toEqual([
      {
        cause: "History: TargetUnavailable",
        count: 5,
        last: new Date("2026-10-23T08:00:00Z"),
      },
      { cause: "Run stopped, SQLSTATE 23505", count: 2, last: new Date("2026-10-22T09:00:00Z") },
    ])
  })

  it("counts no run that applied, found nothing, or whose outcome is unknown", () => {
    const only = fromMigration(
      create(MigrationSchema, {
        journal: {
          entries: [
            { type: "RunFinished", at: at("2026-10-23T09:00:00Z"), outcome: "Succeeded" },
            { type: "RunFinished", at: at("2026-10-23T09:10:00Z"), outcome: "UpToDate" },
            { type: "RunFinished", at: at("2026-10-23T10:00:00Z"), outcome: "Unknown" },
            { type: "ApplyFinished", at: at("2026-10-23T11:00:00Z"), outcome: "Unknown" },
          ],
        },
      })
    )
    expect(failureCauses(only)).toEqual([])
  })

  it("names a MySQL error by its number when there is no SQLSTATE", () => {
    const mysql = fromMigration(
      create(MigrationSchema, {
        journal: {
          entries: [
            {
              type: "RunFinished",
              at: at("2026-10-23T09:00:00Z"),
              outcome: "Failed",
              failure: { version: 3n, errorNumber: 1062 },
            },
          ],
        },
      })
    )
    expect(failureCauses(mysql).map((cause) => cause.cause)).toEqual(["Run stopped, error 1062"])
  })
})

describe("Related resources", () => {
  it("names what the status names, and a Secret only by its key", () => {
    const schema = fromSchema(
      create(SchemaSchema, {
        ref: { namespace: "application", name: "storefront", uid: "uid-schema" },
        source: { requested: "oci://registry.example/schemas/storefront:1", digest: "sha256:aa" },
        target: {
          engine: "postgres",
          secretName: "storefront-db",
          secretKey: "dsn",
          realm: "shop",
        },
        currentPlan: {
          name: "storefront-p-2",
          uid: "uid-plan-2",
          approval: { name: "approve-2", approver: "alice" },
        },
        applied: { planName: "storefront-p-1" },
        activeOperation: { type: "Apply", jobName: "storefront-apply-2", attempt: 1 },
      })
    )
    expect(relatedResources(schema)).toEqual([
      { kind: "Plan", name: "storefront-p-2", note: "current" },
      { kind: "Approval", name: "approve-2", note: "by alice" },
      { kind: "Plan", name: "storefront-p-1", note: "applied" },
      { kind: "Job", name: "storefront-apply-2", note: "Apply, attempt 1" },
      {
        kind: "Source",
        name: "oci://registry.example/schemas/storefront:1",
        note: "pinned by digest",
      },
      { kind: "Secret", name: "storefront-db", note: "key dsn; the console never reads it" },
      { kind: "PtahRealm", name: "shop", note: "grants the database to this namespace" },
    ])
  })

  it("names a plan that is both current and applied once", () => {
    const schema = fromSchema(
      create(SchemaSchema, {
        currentPlan: { name: "storefront-p-1", uid: "uid-plan-1" },
        applied: { planName: "storefront-p-1" },
      })
    )
    expect(relatedResources(schema)).toEqual([
      { kind: "Plan", name: "storefront-p-1", note: "current and applied" },
    ])
  })
})

describe("The last outcome", () => {
  it("is the newest Apply or verification the journal records", () => {
    const schema = fromSchema(
      create(SchemaSchema, {
        journal: {
          entries: [
            { type: "ApplyFinished", at: at("2026-10-23T09:00:00Z"), outcome: "Succeeded" },
            { type: "Verified", at: at("2026-10-23T09:05:00Z"), outcome: "DriftRemains" },
            { type: "PlanPublished", at: at("2026-10-23T10:00:00Z"), plan: "storefront-p-3" },
          ],
        },
        applied: { planName: "storefront-p-1", completedAt: at("2026-10-01T00:00:00Z") },
      })
    )
    expect(lastOutcome(schema)).toEqual({
      tone: "warn",
      title: "Drift remains after the Apply",
      at: new Date("2026-10-23T09:05:00Z"),
    })
  })

  it.each([
    ["Applied", "ok"],
    ["UpToDate", "ok"],
    ["Failed", "bad"],
    ["Partial", "bad"],
    ["Unknown", "unk"],
  ])("falls back to the last run without a journal (%s)", (outcome, tone) => {
    const migration = fromMigration(
      create(MigrationSchema, {
        lastRun: { outcome, finishedAt: at("2026-10-23T09:00:00Z") },
      })
    )
    expect(lastOutcome(migration)).toEqual({
      tone,
      title: `Last run ${outcome}`,
      at: new Date("2026-10-23T09:00:00Z"),
    })
  })

  it("says nothing was applied when nothing records an outcome", () => {
    expect(lastOutcome(fromSchema(create(SchemaSchema, {})))).toEqual({
      tone: "off",
      title: "Nothing applied yet",
    })
  })
})
