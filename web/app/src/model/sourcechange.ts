import { SourceKind } from "../gen/ptah/console/v1/resources_pb"
import type { ResourceView } from "./resource"

/**
 * What a change of source would do to a resource, worked out from what the
 * page read: the field it sets, the reference it replaces, whether it can be
 * sent now, and what the operator does next. The words follow kubectl ptah
 * set-source and the operator's guide to moving to a new artifact.
 */
export interface SourceChange {
  /** The object the change is for; another object under the name is not changed. */
  uid: string
  /** spec.desired.ociRef of a schema, spec.artifact.ociRef of a migration. */
  field: string
  /** The reference the spec names now, which the change replaces. */
  current: string
  /** Why the change cannot be sent now; empty when it can. */
  blocked: string
  /** What follows the change, one sentence each. */
  next: string[]
}

/**
 * The change of source a resource offers, or nothing for one whose source is
 * a ConfigMap or Git: moving to an artifact also names the verification
 * policy it is held to, which is an edit of the resource.
 */
export function sourceChange(resource: ResourceView): SourceChange | undefined {
  const message = resource.schema ?? resource.migration
  const source = message?.source
  if (source?.kind !== SourceKind.OCI || !source.specReference) {
    return undefined
  }
  const schema = resource.kind === "schema"
  const operation = message?.activeOperation
  const applying = operation?.type === "Apply"
  return {
    uid: resource.ref.uid,
    field: schema ? "spec.desired.ociRef" : "spec.artifact.ociRef",
    current: source.specReference,
    blocked:
      applying && operation.dispatched
        ? `An Apply is running (Job ${operation.jobName}). A spec change now moves the generation the Apply was decided from, and the operator records an outcome nobody established without reading its result${schema ? "" : ": for a migration, a run nobody accounted for, which takes a person's acknowledgment once the new versions are pending"}. Wait for the Apply to end, then change the source.`
        : "",
    next: nextSteps(
      resource,
      applying ? (operation.dispatched ? "" : "Apply") : (operation?.type ?? "")
    ),
  }
}

function nextSteps(resource: ResourceView, running: string): string[] {
  const schema = resource.kind === "schema"
  const reads = schema
    ? "reads the database and plans from what it finds"
    : "reads the migration history and plans the versions still pending"
  const steps = [
    resource.suspended
      ? `The resource is suspended: the operator starts no Job until it is resumed, so nothing resolves the new reference before then. Once it is resumed, the operator resolves the new reference, checks it against the verification policy and ${reads}.`
      : `Once the operator observes the new generation, it resolves the new reference, checks it against the verification policy and ${reads}.`,
    "A plan or an approval from the previous artifact does not carry over, and an artifact that asks for nothing the database lacks leads to no plan.",
    applyPolicyStep(resource),
  ]
  if (schema && resource.schema?.allowDestructive === false && resource.applyPolicy !== "Never") {
    steps.push(
      "A destructive plan is held while spec.policy.allowDestructive is false: the operator asks for no approval and applies nothing (DestructiveChangesDisabled) until the spec changes."
    )
  }
  // One of these, as the operator's plugin says it: a proof still owed
  // outranks the new generation, so the operation that takes it is not
  // discarded.
  if (schema && resource.schema?.pendingObservation) {
    steps.push(
      "The proof that the last Apply converged comes first: it outranks a newer generation, and the new reference is resolved after it."
    )
  } else if (running === "Apply") {
    steps.push(
      "The Apply claimed from the previous spec has no Job yet: the operator drops it when it sees the change, and its plan does not run."
    )
  } else if (running) {
    steps.push(
      `The ${running} operation in flight was decided from the previous spec; the operator discards its result once its Job ends.`
    )
  }
  if (!schema && resource.unresolvedRun) {
    steps.push(
      "A run nobody accounted for holds this migration, and while that record stands it publishes no plan from any artifact."
    )
  }
  steps.push(
    "A change of source is a spec change: where the installation requires a distinct approver, you become the resource's last spec writer and cannot approve the plan that follows."
  )
  if (resource.gitops?.controller) {
    steps.push(`${resource.gitops.controller} manages this object; its next sync may set it back.`)
  }
  return steps
}

/** What the apply policy lets the new plan do, in the operator's words. */
function applyPolicyStep(resource: ResourceView) {
  switch (resource.applyPolicy) {
    case "Always":
      return resource.kind === "schema"
        ? "Under spec.policy.apply Always the operator applies that plan without a person, unless it is destructive or changes privileges: those wait for an approval."
        : "Under spec.policy.apply Always that plan runs as soon as it is published, without a person."
    case "Never":
      return "Under spec.policy.apply Never the plan is recorded and nothing applies it (ApplyDisabled)."
    default:
      return "Under spec.policy.apply OnApproval that plan waits for an approval."
  }
}
