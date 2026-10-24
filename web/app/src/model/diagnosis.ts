import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import { activity } from "./activity"
import type { ResourceView } from "./resource"

const date = (at: Timestamp | undefined) => (at ? timestampDate(at) : undefined)

/** One cause of failure the journal records, how often, and when it last happened. */
export interface FailureCause {
  cause: string
  count: number
  last: Date
}

/**
 * What failed, by operation and reason for a failed attempt and by where it
 * stopped for a run, over the period the journal covers. Attempts that failed
 * alike in a row count as many times as the journal folded.
 */
export function failureCauses(resource: ResourceView): FailureCause[] {
  const journal = resource.schema?.journal ?? resource.migration?.journal
  const causes = new Map<string, FailureCause>()
  for (const entry of journal?.entries ?? []) {
    let cause: string | undefined
    let count = 1
    if (entry.type === "OperationFailed") {
      cause = `${entry.operation || "An operation"}: ${entry.reason}`
      count = Math.max(1, entry.count)
    } else if (
      entry.type === "RunFinished" &&
      (entry.outcome === "Failed" || entry.outcome === "Partial")
    ) {
      cause = entry.failure?.sqlState
        ? `Run stopped, SQLSTATE ${entry.failure.sqlState}`
        : entry.failure?.errorNumber
          ? `Run stopped, error ${entry.failure.errorNumber}`
          : "Run stopped"
    }
    const at = date(entry.lastAt) ?? date(entry.at)
    if (!cause || !at) {
      continue
    }
    const seen = causes.get(cause) ?? { cause, count: 0, last: at }
    seen.count += count
    if (at > seen.last) {
      seen.last = at
    }
    causes.set(cause, seen)
  }
  return [...causes.values()].sort(
    (a, b) => b.count - a.count || b.last.getTime() - a.last.getTime()
  )
}

/** An object the resource's status names, and how the page names it. */
export interface Related {
  kind: string
  name: string
  note: string
}

/**
 * The objects the status names, as names only: the console reads none of
 * them here, and a Secret is named by its key, never read.
 */
export function relatedResources(resource: ResourceView): Related[] {
  const related: Related[] = []
  const schema = resource.schema
  const migration = resource.migration
  const appliedPlan = schema?.applied?.planName
  if (resource.plan) {
    related.push({
      kind: "Plan",
      name: resource.plan.name,
      note: appliedPlan === resource.plan.name ? "current and applied" : "current",
    })
  }
  if (schema?.currentPlan?.approval) {
    related.push({
      kind: "Approval",
      name: schema.currentPlan.approval.name,
      note: `by ${schema.currentPlan.approval.approver}`,
    })
  }
  if (appliedPlan && appliedPlan !== resource.plan?.name) {
    related.push({ kind: "Plan", name: appliedPlan, note: "applied" })
  }
  const operation = schema?.activeOperation ?? migration?.activeOperation
  if (operation?.jobName) {
    related.push({
      kind: "Job",
      name: operation.jobName,
      note: `${operation.type}, attempt ${operation.attempt}`,
    })
  }
  if (schema?.pendingObservation?.applyJobName) {
    related.push({
      kind: "Job",
      name: schema.pendingObservation.applyJobName,
      note: "the Apply a fresh read settles",
    })
  }
  if (migration?.lastRun?.jobName) {
    related.push({
      kind: "Job",
      name: migration.lastRun.jobName,
      note: `last run, ${migration.lastRun.outcome}`,
    })
  }
  const source = schema?.source ?? migration?.source
  if (source?.requested) {
    related.push({
      kind: "Source",
      name: source.requested,
      note: source.digest ? "pinned by digest" : "",
    })
  }
  const target = schema?.target ?? migration?.target
  if (target?.secretName) {
    related.push({
      kind: "Secret",
      name: target.secretName,
      note: `key ${target.secretKey}; the console never reads it`,
    })
  }
  if (target?.realm) {
    related.push({
      kind: "PtahRealm",
      name: target.realm,
      note: "grants the database to this namespace",
    })
  }
  return related
}

/** The newest outcome the journal or the status records, for the page's summary. */
export function lastOutcome(resource: ResourceView): { tone: Tone; title: string; at?: Date } {
  const [newest] = activity([resource], []).filter(
    (entry) => entry.type === "apply" || entry.type === "verification"
  )
  if (newest) {
    return { tone: newest.tone, title: newest.title, at: newest.at }
  }
  const run = resource.migration?.lastRun
  if (run) {
    return {
      tone:
        run.outcome === "Applied" || run.outcome === "UpToDate"
          ? "ok"
          : run.outcome === "Failed" || run.outcome === "Partial"
            ? "bad"
            : "unk",
      title: `Last run ${run.outcome}`,
      at: date(run.finishedAt),
    }
  }
  const applied = resource.schema?.applied
  if (applied) {
    return { tone: "ok", title: "Applied and verified", at: date(applied.completedAt) }
  }
  return { tone: "off", title: "Nothing applied yet" }
}
