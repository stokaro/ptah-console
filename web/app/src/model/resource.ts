import type { Timestamp } from "@bufbuild/protobuf/wkt"
import { timestampDate } from "@bufbuild/protobuf/wkt"
import type { Finding, Step, Tone } from "@ptah/console-ui"

import {
  AttentionKind,
  SqlAccess,
  StageState,
  type Attention,
  type Condition,
  type Migration,
  type ObjectRef,
  type Schema,
  type SchemaPlan,
  type Stage,
  type Statement,
} from "../gen/ptah/console/v1/resources_pb"

export type ResourceKind = "schema" | "migration"

/** A schema or a migration, read the same way by every screen. */
export interface ResourceView {
  kind: ResourceKind
  ref: ObjectRef
  namespace: string
  name: string
  phase: string
  statusCurrent: boolean
  suspended: boolean
  applyPolicy: string
  stages: Stage[]
  attention: Attention[]
  conditions: Condition[]
  /** The source as requested: an OCI reference, a ConfigMap or a Git repository. */
  source: string
  /** The OCI manifest digest, or the digest of the files a ConfigMap or Git source selected. */
  sourceDigest: string
  plan?: {
    name: string
    uid: string
    fingerprint: string
    createdAt?: Timestamp
    destructive: boolean
    privilegeChanges: string[]
    statementCount: number
  }
  /**
   * Whether the operator's admission admits an approval of the plan now. A
   * resource waiting for approval refuses one while the operator reads it
   * again at its interval, its plan still current.
   */
  approvalOpen: boolean
  gitops?: { controller: string; application: string }
  /** The plan the database last received, for a schema. */
  appliedPlanUid?: string
  /** The run a migration waits on a person to settle. */
  unresolvedRun?: { operationId: string; outcome: string; jobName: string }
  /** The message as read, for a page that shows more than every screen needs. */
  schema?: Schema
  migration?: Migration
}

export function fromSchema(schema: Schema): ResourceView {
  const plan = schema.currentPlan
  return {
    kind: "schema",
    ref: schema.ref!,
    namespace: schema.ref?.namespace ?? "",
    name: schema.ref?.name ?? "",
    phase: schema.phase,
    statusCurrent: schema.statusCurrent,
    suspended: schema.suspended,
    applyPolicy: schema.applyPolicy,
    stages: schema.stages,
    attention: schema.attention,
    conditions: schema.conditions,
    source: schema.source?.requested ?? "",
    sourceDigest: schema.source?.digest || schema.source?.contentDigest || "",
    plan: plan
      ? {
          name: plan.name,
          uid: plan.uid,
          fingerprint: plan.fingerprint,
          createdAt: plan.createdAt,
          destructive: plan.destructive,
          privilegeChanges: plan.privilegeChanges,
          statementCount: plan.statementCount,
        }
      : undefined,
    approvalOpen: schema.approvalOpen,
    gitops: schema.gitops,
    appliedPlanUid: schema.applied?.planUid,
    schema,
  }
}

export function fromMigration(migration: Migration): ResourceView {
  return {
    kind: "migration",
    ref: migration.ref!,
    namespace: migration.ref?.namespace ?? "",
    name: migration.ref?.name ?? "",
    phase: migration.phase,
    statusCurrent: migration.statusCurrent,
    suspended: migration.suspended,
    applyPolicy: migration.applyPolicy,
    stages: migration.stages,
    attention: migration.attention,
    conditions: migration.conditions,
    source: migration.source?.requested ?? "",
    sourceDigest: migration.source?.digest || migration.source?.contentDigest || "",
    plan: migration.planName
      ? {
          name: migration.planName,
          uid: migration.planUid,
          fingerprint: "",
          destructive: false,
          privilegeChanges: [],
          statementCount: 0,
        }
      : undefined,
    approvalOpen: migration.approvalOpen,
    gitops: migration.gitops,
    unresolvedRun: migration.unresolvedRun
      ? {
          operationId: migration.unresolvedRun.operationId,
          outcome: migration.unresolvedRun.outcome,
          jobName: migration.unresolvedRun.jobName,
        }
      : undefined,
    migration,
  }
}

const stageTones: Record<number, Tone> = {
  [StageState.DONE]: "ok",
  [StageState.ACTIVE]: "run",
  [StageState.WAITING]: "warn",
  [StageState.FAILED]: "bad",
  [StageState.UNKNOWN]: "unk",
  [StageState.NOT_APPLICABLE]: "off",
}

export function stageTone(state: StageState): Tone {
  return stageTones[state] ?? "off"
}

export function steps(resource: ResourceView): Step[] {
  return resource.stages.map((stage) => ({
    label: stage.name,
    tone: stageTone(stage.state),
    detail: stage.detail,
  }))
}

const attentionTones: Record<number, Tone> = {
  [AttentionKind.WAITING_FOR_APPROVAL]: "warn",
  [AttentionKind.FAILED]: "bad",
  [AttentionKind.BLOCKED]: "bad",
  [AttentionKind.OUTCOME_UNKNOWN]: "unk",
  [AttentionKind.UNRESOLVED_RUN]: "unk",
  [AttentionKind.DRIFT]: "warn",
  [AttentionKind.SUSPENDED]: "off",
  [AttentionKind.STALE_STATUS]: "unk",
}

const attentionTitles: Record<number, string> = {
  [AttentionKind.WAITING_FOR_APPROVAL]: "Waiting for approval",
  [AttentionKind.FAILED]: "Failed",
  [AttentionKind.BLOCKED]: "Blocked",
  [AttentionKind.OUTCOME_UNKNOWN]: "Outcome unknown",
  [AttentionKind.UNRESOLVED_RUN]: "Unresolved run",
  [AttentionKind.DRIFT]: "Drift",
  [AttentionKind.SUSPENDED]: "Suspended",
  [AttentionKind.STALE_STATUS]: "Status is stale",
}

export function attentionTone(kind: AttentionKind): Tone {
  return attentionTones[kind] ?? "off"
}

export function attentionTitle(kind: AttentionKind): string {
  return attentionTitles[kind] ?? "Needs a person"
}

/** Kinds that bring a resource to the on-call list. */
const incidentKinds = new Set<number>([
  AttentionKind.FAILED,
  AttentionKind.BLOCKED,
  AttentionKind.OUTCOME_UNKNOWN,
  AttentionKind.UNRESOLVED_RUN,
  AttentionKind.DRIFT,
  AttentionKind.STALE_STATUS,
])

export function incident(resource: ResourceView): Attention | undefined {
  return resource.attention.find((a) => incidentKinds.has(a.kind))
}

export function waitingForApproval(resource: ResourceView): Attention | undefined {
  return resource.attention.find((a) => a.kind === AttentionKind.WAITING_FOR_APPROVAL)
}

/**
 * Why a plan waiting for approval cannot be approved this moment: the
 * operator is reading the resource again, as it does each interval, and its
 * admission refuses an approval until the read ends.
 */
export function readingAgain(resource: ResourceView): string {
  const what = resource.kind === "schema" ? resource.name : `the history of ${resource.name}`
  return `The operator is reading ${what} again, as it does each interval. Approval opens when the read ends, unless the read replaces the plan.`
}

/** One sentence on where a resource stands. */
export function say(resource: ResourceView, now: Date): { tone: Tone; text: string } {
  const first = resource.attention[0]
  if (first) {
    return { tone: attentionTone(first.kind), text: first.reason }
  }
  const failed = resource.stages.find((s) => s.state === StageState.FAILED)
  if (failed) {
    return { tone: "bad", text: sentence(failed.detail) || `${failed.name} failed.` }
  }
  const active = resource.stages.find((s) => s.state === StageState.ACTIVE)
  if (active) {
    return { tone: "run", text: sentence(active.detail) || `${active.name} is in progress.` }
  }
  const allDone =
    resource.stages.length > 0 &&
    resource.stages.every(
      (s) => s.state === StageState.DONE || s.state === StageState.NOT_APPLICABLE
    )
  if (allDone) {
    const verified = resource.stages[resource.stages.length - 1]
    const synced = resource.conditions.find((c) => c.type === "InSync" && c.status === "True")
    const when = synced?.lastTransition ? `, ${age(synced.lastTransition, now)} ago` : ""
    return {
      tone: "ok",
      text: sentence(verified?.detail) || `In the database and verified by a fresh read${when}.`,
    }
  }
  return { tone: "off", text: resource.phase ? `Phase ${resource.phase}.` : "No status yet." }
}

/** What a plan changes, as the plan itself reports it. */
export function planRisks(resource: ResourceView, plan?: SchemaPlan): Finding[] {
  const risks: Finding[] = []
  const destructive = plan?.destructive ?? resource.plan?.destructive ?? false
  const privileges = plan?.privilegeChanges ?? resource.plan?.privilegeChanges ?? []
  if (destructive) {
    risks.push({
      tone: "bad",
      text: "Destructive: removes or rewrites data, as reported by the plan",
    })
  }
  for (const change of privileges) {
    risks.push({ tone: "unk", text: `Changes privileges: ${change}` })
  }
  for (const change of plan?.unrecordedPrivilegeChanges ?? []) {
    risks.push({ tone: "unk", text: `Changes privileges outside the recorded set: ${change}` })
  }
  if (!destructive && privileges.length === 0 && resource.kind === "schema") {
    risks.push({ tone: "ok", text: "No destructive statement and no privilege change" })
  }
  if (plan) {
    risks.push(sqlRisk(plan))
  } else if (resource.kind === "migration") {
    risks.push({ tone: "off", text: "SQL is not available through this operator API" })
  }
  return risks
}

function sqlRisk(plan: SchemaPlan): Finding {
  switch (plan.sqlAccess) {
    case SqlAccess.AVAILABLE: {
      const chunks = plan.chunks.length
      return {
        tone: "ok",
        text: `SQL verified · ${chunks} of ${chunks} ${chunks === 1 ? "chunk" : "chunks"} · digests match`,
      }
    }
    case SqlAccess.NOT_GRANTED:
      return { tone: "off", text: "You may not read this plan's SQL" }
    case SqlAccess.INTEGRITY_FAILED:
      return { tone: "bad", text: "The stored plan failed a check; none of its SQL is shown" }
    case SqlAccess.UNSUPPORTED:
      return { tone: "unk", text: "The plan was written in a form this console does not read" }
    default:
      return { tone: "unk", text: "Whether the SQL is readable is not known" }
  }
}

const severityTones: Record<string, Tone> = {
  destructive: "bad",
  error: "bad",
  warning: "warn",
  info: "off",
}

/** The marker a statement carries, from the plan's own severity. */
export function statementTag(statement: Statement): { tag?: string; tone?: Tone } {
  const severity = statement.severity.toLowerCase()
  if (!severity || severity === "safe") {
    return {}
  }
  return { tag: statement.reason || severity, tone: severityTones[severity] ?? "off" }
}

/** How long ago, in the largest whole unit. */
export function age(at: Timestamp | undefined, now: Date): string {
  if (!at) {
    return "–"
  }
  const seconds = Math.max(0, Math.round((now.getTime() - timestampDate(at).getTime()) / 1000))
  if (seconds < 60) {
    return `${seconds} s`
  }
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    return `${minutes} min`
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 48) {
    return `${hours} h`
  }
  return `${Math.floor(hours / 24)} d`
}

/** A wall-clock time for a timeline, in the reader's time zone. */
export function clock(at: Timestamp | undefined): string {
  if (!at) {
    return "–"
  }
  return timestampDate(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

/** The path segment for a kind. */
export function kindSegment(kind: ResourceKind) {
  return kind
}

export function parseKind(segment: string): ResourceKind | undefined {
  return segment === "schema" || segment === "migration" ? segment : undefined
}

/** A condition's place on a timeline. */
export function conditionTone(condition: Condition): Tone {
  if (condition.status === "Unknown") {
    return "unk"
  }
  const on = condition.status === "True"
  switch (condition.type) {
    case "ReconciliationFailed":
    case "Blocked":
      return on ? "bad" : "ok"
    case "ApprovalRequired":
    case "DriftDetected":
      return on ? "warn" : "ok"
    case "Suspended":
      return on ? "off" : "ok"
    case "Applying":
    case "Progressing":
      return on ? "run" : "ok"
    default:
      return on ? "ok" : "warn"
  }
}

/** Conditions as a timeline, oldest first. */
export function history(resource: ResourceView) {
  return [...resource.conditions].sort(
    (a, b) => Number(a.lastTransition?.seconds ?? 0n) - Number(b.lastTransition?.seconds ?? 0n)
  )
}

/** Quotes a CSV field, and keeps a spreadsheet from reading it as a formula. */
export function csvField(value: string) {
  const guarded = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value
  return /[",\n\r]/.test(guarded) ? `"${guarded.replaceAll('"', '""')}"` : guarded
}

/** A stage's detail as a sentence: capitalized, with a full stop. */
export function sentence(detail: string | undefined) {
  const text = detail?.trim() ?? ""
  if (text === "") {
    return ""
  }
  const capitalized = text.charAt(0).toUpperCase() + text.slice(1)
  return /[.!?]$/.test(capitalized) ? capitalized : `${capitalized}.`
}
