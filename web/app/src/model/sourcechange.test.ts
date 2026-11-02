import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import {
  MigrationSchema,
  OperationSchema,
  PendingObservationSchema,
  SchemaSchema,
  SourceKind,
  UnresolvedRunSchema,
  type Migration,
  type Schema,
} from "../gen/ptah/console/v1/resources_pb"
import { fromMigration, fromSchema } from "./resource"
import { sourceChange } from "./sourcechange"

const current = `oci://registry.example/shop/storefront@sha256:${"a".repeat(64)}`

function schema(change: (schema: Schema) => void = () => {}) {
  const message = create(SchemaSchema, {
    ref: { namespace: "application", name: "storefront", uid: "uid-schema" },
    applyPolicy: "OnApproval",
    allowDestructive: true,
    source: { kind: SourceKind.OCI, requested: current, specReference: current },
  })
  change(message)
  return fromSchema(message)
}

function migration(change: (migration: Migration) => void = () => {}) {
  const message = create(MigrationSchema, {
    ref: { namespace: "application", name: "billing", uid: "uid-migration" },
    applyPolicy: "OnApproval",
    source: { kind: SourceKind.OCI, requested: current, specReference: current },
  })
  change(message)
  return fromMigration(message)
}

describe("sourceChange", () => {
  it("names the field a schema and a migration keep their reference in", () => {
    expect(sourceChange(schema())).toMatchObject({
      field: "spec.desired.ociRef",
      current,
      blocked: "",
    })
    expect(sourceChange(migration())).toMatchObject({ field: "spec.artifact.ociRef", current })
  })

  it("replaces what the spec names, not what the operator last resolved", () => {
    const next = `oci://registry.example/shop/storefront@sha256:${"b".repeat(64)}`
    expect(sourceChange(schema((s) => (s.source!.specReference = next)))?.current).toBe(next)
  })

  it.each([
    ["a ConfigMap", SourceKind.CONFIG_MAP],
    ["a Git repository", SourceKind.GIT],
    ["a source the console does not know", SourceKind.UNSPECIFIED],
  ])("offers nothing for %s", (_, kind) => {
    expect(sourceChange(schema((s) => (s.source!.kind = kind)))).toBeUndefined()
  })

  it.each([
    ["OnApproval", "schema", "Under spec.policy.apply OnApproval that plan waits for an approval."],
    [
      "Always",
      "schema",
      "Under spec.policy.apply Always the operator applies that plan without a person, unless it is destructive or changes privileges: those wait for an approval.",
    ],
    [
      "Always",
      "migration",
      "Under spec.policy.apply Always that plan runs as soon as it is published, without a person.",
    ],
    [
      "Never",
      "migration",
      "Under spec.policy.apply Never the plan is recorded and nothing applies it (ApplyDisabled).",
    ],
  ])("says what apply %s lets a %s's plan do", (policy, kind, says) => {
    const resource =
      kind === "schema"
        ? schema((s) => (s.applyPolicy = policy))
        : migration((m) => (m.applyPolicy = policy))
    expect(sourceChange(resource)?.next).toContain(says)
  })

  it("says nothing resolves the new reference before a suspended resource is resumed", () => {
    const next = sourceChange(schema((s) => (s.suspended = true)))?.next ?? []
    expect(next[0]).toMatch(
      /^The resource is suspended: the operator starts no Job until it is resumed/
    )
    expect(sourceChange(schema())?.next[0]).toMatch(
      /^Once the operator observes the new generation/
    )
  })

  it("says a destructive plan is held while allowDestructive is false, and only then", () => {
    const held = "DestructiveChangesDisabled"
    expect(sourceChange(schema((s) => (s.allowDestructive = false)))?.next.join(" ")).toContain(
      held
    )
    expect(sourceChange(schema())?.next.join(" ")).not.toContain(held)
    expect(
      sourceChange(
        schema((s) => {
          s.allowDestructive = false
          s.applyPolicy = "Never"
        })
      )?.next.join(" ")
    ).not.toContain(held)
  })

  it("holds the change back under an Apply whose Job may exist, and not under one claimed without", () => {
    const dispatched = sourceChange(
      schema((s) => {
        s.activeOperation = create(OperationSchema, {
          type: "Apply",
          jobName: "ptah-apply",
          dispatched: true,
        })
      })
    )
    const claimed = sourceChange(
      migration((m) => {
        m.activeOperation = create(OperationSchema, { type: "Apply", dispatched: false })
      })
    )
    const reading = sourceChange(
      schema((s) => {
        s.activeOperation = create(OperationSchema, { type: "Observe", dispatched: true })
      })
    )

    expect(dispatched?.blocked).toMatch(/^An Apply is running \(Job ptah-apply\)/)
    expect(claimed?.blocked).toBe("")
    expect(claimed?.next).toContain(
      "The Apply claimed from the previous spec has no Job yet: the operator drops it when it sees the change, and its plan does not run."
    )
    expect(reading?.blocked).toBe("")
    expect(reading?.next).toContain(
      "The Observe operation in flight was decided from the previous spec; the operator discards its result once its Job ends."
    )
  })

  it("says a migration that waits on an unresolved run publishes no plan", () => {
    const waiting = sourceChange(
      migration((m) => {
        m.unresolvedRun = create(UnresolvedRunSchema, { operationId: "op-1", outcome: "Unknown" })
      })
    )
    expect(waiting?.next.join(" ")).toContain("A run nobody accounted for holds this migration")
    expect(sourceChange(migration())?.next.join(" ")).not.toContain("A run nobody accounted for")
  })

  it("says a proof still owed comes first, and does not call its operation discarded", () => {
    const next =
      sourceChange(
        schema((s) => {
          s.pendingObservation = create(PendingObservationSchema, { outcome: "Succeeded" })
          s.activeOperation = create(OperationSchema, { type: "Observe", dispatched: true })
        })
      )?.next.join(" ") ?? ""

    expect(next).toContain("The proof that the last Apply converged comes first")
    expect(next).not.toContain("discards its result")
  })

  it("is for the object it was worked out from", () => {
    expect(sourceChange(schema())?.uid).toBe("uid-schema")
  })
})
