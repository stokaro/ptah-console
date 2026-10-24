import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import { DecisionKind, type Decision } from "../gen/ptah/console/v1/decisions_pb"
import type { JournalEntry } from "../gen/ptah/console/v1/resources_pb"
import { decisionState } from "./decision"
import type { ResourceKind, ResourceView } from "./resource"

/** What an entry is about, as the type filter groups them. */
export type ActivityType =
  "plan" | "approval" | "apply" | "verification" | "acknowledgment" | "failure"

export const activityTypes: { value: ActivityType; label: string }[] = [
  { value: "plan", label: "Plans" },
  { value: "approval", label: "Approvals" },
  { value: "apply", label: "Applies and runs" },
  { value: "verification", label: "Verifications" },
  { value: "acknowledgment", label: "Acknowledgments" },
  { value: "failure", label: "Failures" },
]

/** One thing that happened, from a journal entry or a decision. */
export interface ActivityEntry {
  key: string
  type: ActivityType
  at: Date
  kind: ResourceKind
  namespace: string
  name: string
  tone: Tone
  title: string
  /** The person who decided, as admission recorded it. */
  who?: string
  /** How long an Apply or a run took. */
  durationMs?: number
  /** What a reader opens the entry for, one fact a line. */
  details: string[]
}

function date(timestamp: Timestamp | undefined) {
  return timestamp ? timestampDate(timestamp) : undefined
}

function plural(count: number, one: string, many = `${one}s`) {
  return `${count} ${count === 1 ? one : many}`
}

/** Where a migration run stopped, without the engine's message. */
function stoppedAt(entry: JournalEntry) {
  const failure = entry.failure
  if (!failure) {
    return undefined
  }
  const parts = [`migration ${failure.version}`]
  if (failure.statements > 0) {
    parts.push(`statement ${failure.statement} of ${failure.statements}`)
  }
  if (failure.sqlState) {
    parts.push(`SQLSTATE ${failure.sqlState}`)
  }
  if (failure.errorNumber) {
    parts.push(`error ${failure.errorNumber}`)
  }
  return parts.join(", ")
}

const runs: Record<string, { tone: Tone; title: (entry: JournalEntry) => string }> = {
  Succeeded: { tone: "ok", title: (e) => `Run applied ${plural(e.appliedVersions, "version")}` },
  UpToDate: { tone: "ok", title: () => "Run found nothing to apply" },
  Failed: { tone: "bad", title: (e) => `Run failed at ${stoppedAt(e) ?? "a migration"}` },
  Partial: {
    tone: "bad",
    title: (e) => `Run stopped part-way, at ${stoppedAt(e) ?? "a migration"}`,
  },
  Unknown: { tone: "unk", title: () => "Run outcome unknown" },
}

const verifications: Record<string, { tone: Tone; title: string }> = {
  InSync: { tone: "ok", title: "Verified in sync" },
  DriftRemains: { tone: "warn", title: "Drift remains after the Apply" },
  Superseded: { tone: "off", title: "Verification superseded by a newer spec or binding" },
}

/**
 * A journal entry as the feed reads it. An acceptance or an acknowledgment a
 * listed decision already stands for returns nothing: the decision says who
 * and when.
 */
function fromJournal(
  resource: ResourceView,
  entry: JournalEntry,
  index: number,
  decided: Set<string>
) {
  const at = date(entry.at)
  if (!at) {
    return undefined
  }
  const base = {
    key: `${resource.kind}/${resource.namespace}/${resource.name}/journal/${index}`,
    at,
    kind: resource.kind,
    namespace: resource.namespace,
    name: resource.name,
  }
  const started = date(entry.startedAt)
  const durationMs = started ? at.getTime() - started.getTime() : undefined
  const plan = entry.plan ? [`Plan ${entry.plan}`] : []
  switch (entry.type) {
    case "PlanPublished":
      return {
        ...base,
        type: "plan" as const,
        tone: (entry.destructive ? "warn" : "off") as Tone,
        title: entry.destructive ? "Destructive plan published" : "Plan published",
        details: [...plan, plural(entry.statements, "statement")],
      }
    case "ApprovalAccepted":
      if (decided.has(`${resource.namespace}/${entry.approval}`)) {
        return undefined
      }
      return {
        ...base,
        type: "approval" as const,
        tone: "ok" as Tone,
        title: entry.approver ? `Approval by ${entry.approver} accepted` : "Approval accepted",
        who: entry.approver || undefined,
        details: [...plan, `Approval ${entry.approval}`],
      }
    case "ApplyFinished":
      return {
        ...base,
        type: "apply" as const,
        tone: (entry.outcome === "Succeeded" ? "ok" : "unk") as Tone,
        title: entry.outcome === "Succeeded" ? "Apply succeeded" : "Apply outcome unknown",
        durationMs,
        details: [
          ...plan,
          ...(entry.outcome === "Succeeded"
            ? []
            : [
                "A read-only observation establishes what the Apply did; the plan is never run again blindly.",
              ]),
        ],
      }
    case "Verified": {
      const verdict = verifications[entry.outcome] ?? { tone: "unk" as Tone, title: "Verified" }
      return {
        ...base,
        type: "verification" as const,
        tone: verdict.tone,
        title: verdict.title,
        details: plan,
      }
    }
    case "RunFinished": {
      const run = runs[entry.outcome] ?? runs.Unknown
      return {
        ...base,
        type: "apply" as const,
        tone: run.tone,
        title: run.title(entry),
        durationMs,
        details: [
          plural(entry.appliedVersions, "version") + " applied",
          ...(entry.failure ? [`Stopped at ${stoppedAt(entry)}`] : []),
        ],
      }
    }
    case "RunAcknowledged":
      if (decided.has(`${resource.namespace}/${entry.acknowledgment}`)) {
        return undefined
      }
      return {
        ...base,
        type: "acknowledgment" as const,
        tone: "ok" as Tone,
        title: entry.acknowledgedBy
          ? `Run acknowledged by ${entry.acknowledgedBy}`
          : "Run acknowledged",
        who: entry.acknowledgedBy || undefined,
        details: [`Acknowledgment ${entry.acknowledgment}`],
      }
    case "OperationFailed": {
      const last = date(entry.lastAt)
      const times = entry.count > 1 ? `, ${entry.count} times in a row` : ""
      return {
        ...base,
        at: last ?? at,
        type: "failure" as const,
        tone: "bad" as Tone,
        title: `${entry.operation || "An operation"} failed: ${entry.reason}${times}`,
        details:
          entry.count > 1 && last
            ? [`First at ${at.toISOString()}, last at ${last.toISOString()}`]
            : [],
      }
    }
    default:
      return undefined
  }
}

function fromDecision(decision: Decision): ActivityEntry | undefined {
  const at = date(decision.decidedAt)
  if (!at || !decision.ref) {
    return undefined
  }
  const acknowledgment = decision.kind === DecisionKind.RUN_ACKNOWLEDGMENT
  const state = decisionState(decision.state)
  const who = decision.decidedBy || undefined
  return {
    key: `decision/${decision.ref.namespace}/${decision.ref.name}`,
    type: acknowledgment ? "acknowledgment" : "approval",
    at,
    kind: decision.kind === DecisionKind.SCHEMA_APPROVAL ? "schema" : "migration",
    namespace: decision.ref.namespace,
    name: decision.ownerName,
    tone: state.tone,
    title: acknowledgment
      ? `Run acknowledged by ${who ?? "someone"}`
      : `Plan approved by ${who ?? "someone"}`,
    who,
    details: [
      acknowledgment ? `Run ${decision.operationId}` : `Plan ${decision.planName}`,
      `${acknowledgment ? "Acknowledgment" : "Approval"} ${decision.ref.name}, ${state.label}`,
      ...(decision.stateReason ? [decision.stateReason] : []),
    ],
  }
}

/**
 * Everything the readable resources' journals and the listed decisions say
 * happened, newest first. Nothing is made up: an event no journal kept and no
 * decision records is not here.
 */
export function activity(resources: ResourceView[], decisions: Decision[]): ActivityEntry[] {
  const decided = new Set(
    decisions.filter((d) => d.ref).map((d) => `${d.ref!.namespace}/${d.ref!.name}`)
  )
  const entries: ActivityEntry[] = []
  for (const resource of resources) {
    const journal = resource.schema?.journal ?? resource.migration?.journal
    journal?.entries.forEach((entry, index) => {
      const read = fromJournal(resource, entry, index, decided)
      if (read) {
        entries.push(read)
      }
    })
  }
  for (const decision of decisions) {
    const read = fromDecision(decision)
    if (read) {
      entries.push(read)
    }
  }
  return entries.sort((a, b) => b.at.getTime() - a.at.getTime() || a.key.localeCompare(b.key))
}

/** The period the journals of the readable resources cover. */
export interface Coverage {
  /** From when every journal read is complete: the latest coveredSince. */
  complete?: Date
  /** The earliest any journal reaches back to. */
  reaches?: Date
  /** Resources whose operator keeps no journal. */
  withoutJournal: number
}

export function coverage(resources: ResourceView[]): Coverage {
  let complete: Date | undefined
  let reaches: Date | undefined
  let withoutJournal = 0
  for (const resource of resources) {
    const journal = resource.schema?.journal ?? resource.migration?.journal
    const since = date(journal?.coveredSince)
    if (!since) {
      withoutJournal++
      continue
    }
    if (!complete || since > complete) {
      complete = since
    }
    if (!reaches || since < reaches) {
      reaches = since
    }
  }
  return { complete, reaches, withoutJournal }
}

export type ActivityPeriod = "hour" | "day" | "week" | "all"

export const activityPeriods: { value: ActivityPeriod; label: string; ms?: number }[] = [
  { value: "hour", label: "Last hour", ms: 3_600_000 },
  { value: "day", label: "Last 24 hours", ms: 86_400_000 },
  { value: "week", label: "Last 7 days", ms: 7 * 86_400_000 },
  { value: "all", label: "Everything kept" },
]

export interface ActivityFilter {
  type?: ActivityType
  period?: ActivityPeriod
  who?: string
}

export function filterActivity(entries: ActivityEntry[], filter: ActivityFilter, now: Date) {
  const span = activityPeriods.find((period) => period.value === filter.period)?.ms
  return entries.filter(
    (entry) =>
      (!filter.type || entry.type === filter.type) &&
      (!span || now.getTime() - entry.at.getTime() <= span) &&
      (!filter.who || entry.who === filter.who)
  )
}

/** The people who decided something in the feed, for the who filter. */
export function deciders(entries: ActivityEntry[]) {
  return [...new Set(entries.flatMap((entry) => (entry.who ? [entry.who] : [])))].sort()
}
