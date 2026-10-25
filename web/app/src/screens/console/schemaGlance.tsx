import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"
import { Card, StatusGlyph } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { StageState } from "../../gen/ptah/console/v1/resources_pb"
import { activity } from "../../model/activity"
import { ago } from "../../model/connection"
import { driftReading, readingTone } from "../../model/drift"
import { stageTone, waitingForApproval, type ResourceView } from "../../model/resource"
import type { GlanceTile } from "./glance"

function date(timestamp: Timestamp | undefined) {
  return timestamp ? timestampDate(timestamp) : undefined
}

/** In how long a moment comes, or how long ago it passed. */
function until(at: Date, now: Date) {
  const minutes = Math.round((at.getTime() - now.getTime()) / 60_000)
  if (minutes <= 0) {
    return "due"
  }
  return minutes < 60 ? `in ${minutes} min` : `in ${Math.round(minutes / 60)} h`
}

/** The numbers a schema's page puts first: its plan, its drift, its last Apply and its next read. */
export function schemaTiles(resource: ResourceView, now: Date): GlanceTile[] {
  const schema = resource.schema!
  const plan = schema.currentPlan
  const target = schema.target
  const observed = date(target?.lastObservedAt)
  const applied = date(schema.applied?.completedAt)
  const next = date(schema.pendingObservation?.observeAfter) ?? date(schema.nextReconciliation)
  const severity = target?.highestDriftSeverity ?? ""
  const reading = driftReading(target)
  return [
    {
      label: "Current plan",
      value: plan
        ? plan.approval
          ? "Approved"
          : waitingForApproval(resource)
            ? "Waiting"
            : "Published"
        : "None",
      sub: plan
        ? `${plan.statementCount} ${plan.statementCount === 1 ? "statement" : "statements"}${plan.destructive ? ", destructive" : ""}`
        : "No plan is published",
      tone: plan ? (plan.approval ? "ok" : "warn") : "off",
    },
    {
      label: "Drift",
      value: observed ? severity || "None" : "–",
      sub: observed
        ? `${reading.state === "uncategorized" ? "In no category" : `${target?.driftFindingCount ?? 0} ${target?.driftFindingCount === 1 ? "finding" : "findings"}`} at the read ${ago(observed, now)}`
        : "The database is not read yet",
      tone: readingTone(reading),
    },
    {
      label: "Applied",
      value: applied ? ago(applied, now) : "Never",
      sub: applied ? "and verified by a fresh read" : "No plan has reached the database",
      tone: applied ? "ok" : "off",
    },
    {
      label: "Next read",
      value: next ? until(next, now) : "–",
      sub: next ? next.toLocaleTimeString() : "None scheduled",
      tone: "off",
    },
  ]
}

/** Who else changes the resource: a GitOps controller sets its spec back at the next sync. */
export function GitOpsNote({ resource }: { resource: ResourceView }) {
  const gitops = resource.gitops
  if (!gitops?.controller) {
    return null
  }
  return (
    <p className="m-0 text-[13px] text-fg2">
      <span aria-hidden="true">⎇ </span>Managed by {gitops.controller}
      {gitops.application ? ` · ${gitops.application}` : ""}. A change to its spec made here is set
      back at the next sync unless it is made where {gitops.controller} reads it.
    </p>
  )
}

/** What the operator last saw of the database, from the journal, newest first. */
export function RecentObservations({ resource, now }: { resource: ResourceView; now: Date }) {
  const entries = activity([resource], [])
    .filter((entry) => entry.type === "verification" || entry.type === "failure")
    .slice(0, 5)
  return (
    <Card title="Recent observations">
      {entries.length === 0 ? (
        <span className="text-sm text-fg2">
          The journal records no verification or failed read of this schema.
        </span>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
          {entries.map((entry) => (
            <li key={entry.key} className="grid grid-cols-[20px_minmax(0,1fr)_auto] gap-2">
              <StatusGlyph tone={entry.tone} size={20} />
              <span>{entry.title}</span>
              <span className="text-xs whitespace-nowrap text-mute">{ago(entry.at, now)}</span>
            </li>
          ))}
        </ul>
      )}
      <Link
        to="/console/schemas/$namespace/$name/history"
        params={{ namespace: resource.namespace, name: resource.name }}
        className="self-start text-[13px] font-medium text-pri"
      >
        All of its history →
      </Link>
    </Card>
  )
}

const verifyWords: Partial<Record<StageState, string>> = {
  [StageState.DONE]: "Verified",
  [StageState.ACTIVE]: "Verifying",
  [StageState.WAITING]: "Not started",
  [StageState.FAILED]: "Failed",
  [StageState.UNKNOWN]: "Unknown",
  [StageState.NOT_APPLICABLE]: "Not started",
}

/** Whether what was applied was proved by a fresh read, and when the next proof is due. */
export function VerificationCard({ resource, now }: { resource: ResourceView; now: Date }) {
  const verify = resource.stages.find((stage) => stage.name === "Verify")
  const pending = resource.schema?.pendingObservation
  const due = date(pending?.observeAfter)
  const last = activity([resource], []).find((entry) => entry.type === "verification")
  const state = verify?.state ?? StageState.UNKNOWN
  return (
    <Card title="Verification">
      <span className="flex items-center gap-2 text-sm font-semibold">
        <StatusGlyph tone={stageTone(state)} size={20} />
        {verifyWords[state] ?? "Unknown"}
      </span>
      {verify?.detail ? <span className="text-[13px] text-fg2">{verify.detail}</span> : null}
      {pending ? (
        <span className="text-[13px] text-fg2">
          A fresh read settles the Apply{" "}
          {pending.applyJobName ? `of Job ${pending.applyJobName} ` : ""}({pending.outcome})
          {due ? `, ${until(due, now)}` : ""}.
        </span>
      ) : null}
      <span className="text-xs text-mute">
        {last
          ? `Last in the journal: ${last.title}, ${ago(last.at, now)}.`
          : "The journal records no verification yet."}
      </span>
    </Card>
  )
}
