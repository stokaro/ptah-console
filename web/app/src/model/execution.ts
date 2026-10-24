import type { Timestamp } from "@bufbuild/protobuf/wkt"
import { timestampDate } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import {
  Evidence,
  type EventList,
  type GetDiagnosticsResponse,
  type JobEvidence,
} from "../gen/ptah/console/v1/diagnostics_pb"
import type { Condition } from "../gen/ptah/console/v1/resources_pb"
import { activity, whereStopped, type ActivityEntry } from "./activity"
import { conditionTone, type ResourceView } from "./resource"

/** Who left a piece of evidence: the operator's controller, a Job it ran, or the database. */
export type Lane = "controller" | "job" | "database"

export const lanes: { value: Lane; label: string }[] = [
  { value: "controller", label: "Controller" },
  { value: "job", label: "Job" },
  { value: "database", label: "Database" },
]

export interface Entry {
  lane: Lane
  /** When it happened, or undefined for evidence that is gone and kept no time. */
  at?: Date
  /** The instant is a plan for the future rather than a record of the past. */
  due?: boolean
  title: string
  detail: string
  tone: Tone
  /** Kubernetes no longer keeps it, or the identity may not read it. */
  gone?: string
  /** The end of a container's log, as plain text. */
  log?: string
}

const date = (at: Timestamp | undefined) => (at ? timestampDate(at) : undefined)

function eventEntries(lane: Lane, about: string, list: EventList | undefined): Entry[] {
  if (!list) {
    return []
  }
  if (list.evidence !== Evidence.SHOWN) {
    return [
      {
        lane,
        title: `Events about ${about}`,
        detail: list.problem,
        tone: "off",
        gone: goneWord(list.evidence),
      },
    ]
  }
  return list.events.map((event) => ({
    lane,
    at: date(event.lastSeen ?? event.firstSeen),
    title: `${about} · ${event.reason}`,
    detail: event.count > 1 ? `${event.message} (×${event.count})` : event.message,
    tone: event.type === "Warning" ? "bad" : "off",
  }))
}

/** Why a piece of evidence is not shown, in a word a reader can act on. */
export function goneWord(evidence: Evidence) {
  switch (evidence) {
    case Evidence.NOT_GRANTED:
      return "not granted"
    case Evidence.NOT_RETAINED:
      return "not retained"
    case Evidence.UNREADABLE:
      return "unreadable"
    default:
      return "not shown"
  }
}

function jobEntries(job: JobEvidence): Entry[] {
  const about = `Job ${job.name}`
  if (job.evidence !== Evidence.SHOWN) {
    return [
      {
        lane: "job",
        title: about,
        detail: `${job.namedBy} names it. ${job.problem}`.trim(),
        tone: "off",
        gone: goneWord(job.evidence),
      },
    ]
  }
  const entries: Entry[] = []
  const state = job.state
  if (state?.startedAt) {
    entries.push({
      lane: "job",
      at: date(state.startedAt),
      title: `${about} started`,
      detail: job.namedBy,
      tone: "run",
    })
  }
  if (state?.completedAt) {
    entries.push({
      lane: "job",
      at: date(state.completedAt),
      title: `${about} completed`,
      detail: "",
      tone: "ok",
    })
  }
  for (const condition of state?.conditions ?? []) {
    if (condition.type === "Failed" && condition.status === "True") {
      entries.push({
        lane: "job",
        at: date(condition.lastTransition),
        title: `${about} failed`,
        detail: [condition.reason, condition.message].filter(Boolean).join(": "),
        tone: "bad",
      })
    }
  }
  entries.push(...eventEntries("job", about, job.events))
  if (job.podsEvidence !== Evidence.SHOWN && job.podsEvidence !== Evidence.UNSPECIFIED) {
    entries.push({
      lane: "job",
      title: `Pods of ${job.name}`,
      detail: job.podsProblem,
      tone: "off",
      gone: goneWord(job.podsEvidence),
    })
  }
  for (const pod of job.pods) {
    entries.push(...eventEntries("job", `Pod ${pod.name}`, pod.events))
    for (const container of pod.containers) {
      if (container.state !== "terminated" && !container.reason) {
        continue
      }
      const failed = container.state === "terminated" && container.exitCode !== 0
      entries.push({
        lane: "job",
        title: `Container ${container.name}${container.init ? " (init)" : ""} ${container.state}`,
        detail: [
          container.reason,
          container.message,
          container.state === "terminated" ? `exit code ${container.exitCode}` : "",
        ]
          .filter(Boolean)
          .join(" · "),
        tone: failed ? "bad" : container.state === "terminated" ? "ok" : "warn",
        log: container.log?.evidence === Evidence.SHOWN ? container.log.text : undefined,
        gone:
          container.log &&
          container.log.evidence !== Evidence.SHOWN &&
          container.log.evidence !== Evidence.UNSPECIFIED
            ? `log ${goneWord(container.log.evidence)}`
            : undefined,
      })
    }
  }
  return entries
}

function conditionEntry(condition: Condition): Entry {
  return {
    lane: "controller",
    at: date(condition.lastTransition),
    title: `${condition.type} ${condition.status}${condition.reason ? ` · ${condition.reason}` : ""}`,
    detail: condition.message,
    tone: conditionTone(condition),
  }
}

/** What the database was found or made to be, from the resource's own status. */
function databaseEntries(resource: ResourceView): Entry[] {
  const entries: Entry[] = []
  const schema = resource.schema
  const migration = resource.migration
  if (schema?.applied?.completedAt) {
    entries.push({
      lane: "database",
      at: date(schema.applied.completedAt),
      title: `Plan ${schema.applied.planName} applied`,
      detail: `fingerprint ${schema.applied.planFingerprint}`,
      tone: "ok",
    })
  }
  const target = schema?.target ?? migration?.target
  if (target?.lastObservedAt) {
    entries.push({
      lane: "database",
      at: date(target.lastObservedAt),
      title: "Read",
      detail:
        target.driftFindingCount > 0
          ? `${target.driftFindingCount} differences from the artifact${target.highestDriftSeverity ? `, the most severe ${target.highestDriftSeverity}` : ""}`
          : "No difference from the artifact",
      tone: target.driftFindingCount > 0 ? "warn" : "ok",
    })
  }
  if (schema?.pendingObservation?.observeAfter) {
    entries.push({
      lane: "database",
      at: date(schema.pendingObservation.observeAfter),
      due: true,
      title: "Fresh read due",
      detail: `after Apply Job ${schema.pendingObservation.applyJobName} (${schema.pendingObservation.outcome})`,
      tone: "unk",
    })
  }
  const history = migration?.history
  if (history?.observedAt) {
    entries.push({
      lane: "database",
      at: date(history.observedAt),
      title: `History read at version ${history.currentVersion}`,
      detail: `${history.appliedCount} applied, ${history.pendingCount} pending${history.dirty ? ", a dirty row" : ""}`,
      tone: history.dirty ? "bad" : "ok",
    })
  }
  const run = migration?.lastRun
  // A run the journal records is on the timeline from there, with its start.
  const journaled = (migration?.journal?.entries ?? []).some(
    (entry) =>
      entry.type === "RunFinished" &&
      run?.finishedAt &&
      entry.at &&
      Math.abs(timestampDate(entry.at).getTime() - timestampDate(run.finishedAt).getTime()) < 1000
  )
  if (run?.finishedAt && !journaled) {
    entries.push({
      lane: "database",
      at: date(run.finishedAt),
      title: `Run ended ${run.outcome}`,
      detail:
        run.message ||
        (run.appliedVersions.length > 0
          ? `applied ${run.appliedVersions.map(String).join(", ")}`
          : ""),
      tone:
        run.outcome === "Applied" || run.outcome === "UpToDate"
          ? "ok"
          : run.outcome === "Failed"
            ? "bad"
            : "unk",
    })
  }
  return entries
}

/** The lane a journal event belongs on: what the controller decided, or what reached the database. */
const journalLanes: Record<ActivityEntry["type"], Lane> = {
  plan: "controller",
  approval: "controller",
  acknowledgment: "controller",
  failure: "controller",
  apply: "database",
  verification: "database",
}

function seconds(ms: number) {
  return ms < 60_000 ? `${Math.max(1, Math.round(ms / 1000))} s` : `${Math.round(ms / 60_000)} min`
}

/** The resource's journal on the lanes, each event at the time it records. */
function journalEntries(resource: ResourceView): Entry[] {
  return activity([resource], []).map((entry) => ({
    lane: journalLanes[entry.type],
    at: entry.at,
    title: entry.title,
    detail: [
      ...entry.details,
      ...(entry.durationMs !== undefined ? [`took ${seconds(entry.durationMs)}`] : []),
    ].join(" · "),
    tone: entry.tone,
  }))
}

/**
 * Everything the operator and Kubernetes still keep about a resource's work,
 * on three lanes, oldest first; a time still due, such as the next read,
 * comes last of the dated. Evidence that kept no time follows, and evidence
 * that is gone stays as an entry that says why, at the end.
 */
export function timeline(resource: ResourceView, diagnostics?: GetDiagnosticsResponse): Entry[] {
  const entries: Entry[] = [
    ...journalEntries(resource),
    ...resource.conditions.map(conditionEntry),
    ...eventEntries("controller", resource.name, diagnostics?.events),
    ...(diagnostics?.jobs ?? []).flatMap(jobEntries),
    ...databaseEntries(resource),
  ]
  const operation = resource.schema?.activeOperation ?? resource.migration?.activeOperation
  if (operation?.startedAt) {
    entries.push({
      lane: "controller",
      at: date(operation.startedAt),
      title: `${operation.type} claimed, attempt ${operation.attempt}`,
      detail: `operation ${operation.id}, Job ${operation.jobName}`,
      tone: "run",
    })
  }
  const next = resource.schema?.nextReconciliation ?? resource.migration?.nextReconciliation
  if (next) {
    entries.push({
      lane: "controller",
      at: date(next),
      due: true,
      title: "Next reconciliation",
      detail: "",
      tone: "off",
    })
  }
  // Dated evidence first, oldest first; then evidence that kept no time, such
  // as a container's end; then what is gone.
  const rank = (entry: Entry) => (entry.at ? 0 : entry.gone ? 2 : 1)
  const time = (entry: Entry) => entry.at?.getTime() ?? 0
  return entries.sort((a, b) => rank(a) - rank(b) || time(a) - time(b))
}

/**
 * The error that started the trouble, kept in view while the timeline moves
 * on: a run's own account for a migration, the failure condition otherwise,
 * and failing those the first failed container.
 */
export function originalError(resource: ResourceView, diagnostics?: GetDiagnosticsResponse) {
  const run = resource.migration?.lastRun
  if (
    run &&
    (run.outcome === "Failed" || run.outcome === "Partial" || run.outcome === "Unknown") &&
    run.message
  ) {
    return {
      title: `The last run ended ${run.outcome}`,
      text: run.failure ? `${run.message}. Stopped at ${whereStopped(run.failure)}.` : run.message,
      at: date(run.finishedAt),
    }
  }
  const failure = resource.conditions.find(
    (c) => (c.type === "ReconciliationFailed" || c.type === "Blocked") && c.status === "True"
  )
  if (failure) {
    return {
      title: `${failure.type} · ${failure.reason}`,
      text: failure.message,
      at: date(failure.lastTransition),
    }
  }
  for (const job of diagnostics?.jobs ?? []) {
    for (const pod of job.pods) {
      for (const container of pod.containers) {
        if (container.state === "terminated" && container.exitCode !== 0) {
          return {
            title: `Container ${container.name} of Job ${job.name} exited ${container.exitCode}`,
            text: [container.reason, container.message].filter(Boolean).join(": "),
            at: undefined,
          }
        }
      }
    }
  }
  return undefined
}
