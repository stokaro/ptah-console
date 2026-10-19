import { timestampDate } from "@bufbuild/protobuf/wkt"
import { shortDigest, type Tone } from "@ptah/console-ui"

import { DecisionKind, DecisionState, type Decision } from "../gen/ptah/console/v1/decisions_pb"
import { csvField } from "./resource"

export type DecisionSegment = "schema-approval" | "migration-approval" | "run-acknowledgment"

const segments: Record<number, DecisionSegment> = {
  [DecisionKind.SCHEMA_APPROVAL]: "schema-approval",
  [DecisionKind.MIGRATION_APPROVAL]: "migration-approval",
  [DecisionKind.RUN_ACKNOWLEDGMENT]: "run-acknowledgment",
}

export function decisionSegment(kind: DecisionKind): DecisionSegment {
  return segments[kind] ?? "schema-approval"
}

export function decisionKind(segment: string): DecisionKind | undefined {
  const entry = Object.entries(segments).find(([, value]) => value === segment)
  return entry ? (Number(entry[0]) as DecisionKind) : undefined
}

const states: Record<number, { tone: Tone; label: string }> = {
  [DecisionState.PENDING]: { tone: "unk", label: "not yet read by the operator" },
  [DecisionState.ACCEPTED]: { tone: "run", label: "accepted" },
  [DecisionState.CONSUMED]: { tone: "ok", label: "consumed" },
  [DecisionState.STALE]: { tone: "warn", label: "stale" },
  [DecisionState.REFUSED]: { tone: "bad", label: "refused" },
}

export function decisionState(state: DecisionState) {
  return states[state] ?? { tone: "off" as Tone, label: "unknown" }
}

/** What was decided, in a few words. */
export function decisionWhat(decision: Decision) {
  if (decision.kind === DecisionKind.RUN_ACKNOWLEDGMENT) {
    return `Acknowledged run ${decision.operationId} of ${decision.ownerName}`
  }
  return `Approved ${decision.planName} for ${decision.ownerName}`
}

export function decisionNote(decision: Decision) {
  const state = decisionState(decision.state).label
  return decision.planFingerprint ? `${shortDigest(decision.planFingerprint)} · ${state}` : state
}

/** Newest first. */
export function byNewest(a: Decision, b: Decision) {
  return Number(b.decidedAt?.seconds ?? 0n) - Number(a.decidedAt?.seconds ?? 0n)
}

/** The decisions as CSV, every field guarded against spreadsheet formulas. */
export function decisionsCsv(decisions: Decision[]) {
  const header = [
    "decided_at",
    "kind",
    "namespace",
    "name",
    "owner",
    "plan",
    "fingerprint",
    "operation",
    "decided_by",
    "state",
  ]
  const rows = decisions.map((d) => [
    d.decidedAt ? timestampDate(d.decidedAt).toISOString() : "",
    decisionSegment(d.kind),
    d.ref?.namespace ?? "",
    d.ref?.name ?? "",
    d.ownerName,
    d.planName,
    d.planFingerprint,
    d.operationId,
    d.decidedBy,
    decisionState(d.state).label,
  ])
  return [header, ...rows].map((row) => row.map(csvField).join(",")).join("\r\n") + "\r\n"
}
