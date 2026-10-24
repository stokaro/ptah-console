import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"
import { ConnectError } from "@connectrpc/connect"
import { skipToken, useQuery } from "@connectrpc/connect-query"
import {
  Card,
  CommandBlock,
  Eyebrow,
  StatusGlyph,
  StepRail,
  Terminal,
  type Tone,
} from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { can, useResource } from "../../api/resources"
import { ResourceKind } from "../../gen/ptah/console/v1/decisions_pb"
import {
  DiagnosticsService,
  Evidence,
  type JobEvidence,
} from "../../gen/ptah/console/v1/diagnostics_pb"
import { StageState } from "../../gen/ptah/console/v1/resources_pb"
import { coverage, duration } from "../../model/activity"
import { ago } from "../../model/connection"
import { failureCauses, lastOutcome } from "../../model/diagnosis"
import { goneWord, jobRank, lastLog } from "../../model/execution"
import {
  incident,
  steps,
  waitingForApproval,
  type ResourceKind as Kind,
  type ResourceView,
} from "../../model/resource"
import { Loading, ReadError, useNow } from "../common"
import { Facts } from "./detail"
import { RelatedResources, ResourceEvents } from "./evidence"
import { resourceLink } from "./links"

export interface ExecutionDiagnosisProps {
  kind: Kind
  namespace: string
  name: string
}

const date = (at: Timestamp | undefined) => (at ? timestampDate(at) : undefined)

/** When a moment is, from now, in words: in 17 seconds, 3 min ago. */
function relativeTo(at: Date, now: Date) {
  const ms = at.getTime() - now.getTime()
  if (ms > 0) {
    return ms < 60_000 ? `in ${Math.round(ms / 1000)} s` : `in ${Math.round(ms / 60_000)} min`
  }
  return ago(at, now)
}

/**
 * The resource's current execution: what runs or ran last and how long it
 * took, the Job's own account and its log, what the database was last seen
 * to be, when it is read next, what the status names, and what a person may
 * do next. The operator retries nothing blindly and rolls nothing back.
 */
export function ExecutionDiagnosis({ kind, namespace, name }: ExecutionDiagnosisProps) {
  const { resource, scope, loading, error } = useResource(kind, namespace, name)
  const now = useNow(5_000)
  const readLogs = resource ? can(scope.scopes, resource.namespace, "readLogs") : false
  const diagnostics = useQuery(
    DiagnosticsService.method.getDiagnostics,
    resource
      ? {
          kind: resource.kind === "schema" ? ResourceKind.SCHEMA : ResourceKind.MIGRATION,
          namespace: resource.namespace,
          name: resource.name,
          uid: resource.ref.uid,
          includeLogs: readLogs,
        }
      : skipToken
  )
  if (loading) {
    return <Loading what="the execution" />
  }
  if (error) {
    return <ReadError error={error} what="the execution" />
  }
  if (!resource) {
    return (
      <Card title="Not found">
        <span className="text-sm">
          No {kind} {name} in {namespace} that this identity may read.
        </span>
      </Card>
    )
  }
  const schema = resource.schema
  const migration = resource.migration
  const operation = schema?.activeOperation ?? migration?.activeOperation
  const started = date(operation?.startedAt)
  const stages = steps(resource)
  const done = resource.stages.filter((stage) => stage.state === StageState.DONE).length
  const outcome = lastOutcome(resource)
  const next =
    date(schema?.pendingObservation?.observeAfter) ??
    date(schema?.nextReconciliation ?? migration?.nextReconciliation)
  const jobs = [...(diagnostics.data?.jobs ?? [])].sort(
    (a, b) => jobRank(a.namedBy) - jobRank(b.namedBy)
  )
  const log = diagnostics.data ? lastLog(diagnostics.data) : undefined
  const causes = failureCauses(resource)
  const kept = coverage([resource])
  const attention = incident(resource) ?? resource.attention[0]
  const planPage =
    resource.kind === "schema"
      ? "/console/schemas/$namespace/$name/plan"
      : "/console/migrations/$namespace/$name/plan"
  const historyPage =
    resource.kind === "schema"
      ? "/console/schemas/$namespace/$name/history"
      : "/console/migrations/$namespace/$name/history"

  return (
    <>
      <section className="flex flex-col gap-2">
        <nav aria-label="Breadcrumb" className="text-xs text-mute">
          <Link to="/console/resources" className="text-mute">
            Resources
          </Link>{" "}
          › {namespace} ›{" "}
          <Link {...resourceLink(resource)} className="text-mute">
            {name}
          </Link>{" "}
          › execution
        </nav>
        <Eyebrow>{resource.kind === "schema" ? "PtahSchema" : "PtahMigration"}</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Execution &amp; diagnosis
        </h1>
        <span className="text-[13.5px] text-fg2">
          Follow the execution, see what the database was last found to be, and what happens next.
        </span>
      </section>

      <section aria-label="Summary" className="grid gap-3 md:grid-cols-3">
        <Summary
          tone={operation ? "run" : "off"}
          title={operation ? `${operation.type} running` : "Nothing running"}
          sub={
            operation && started
              ? `Attempt ${operation.attempt}, started ${relativeTo(started, now)}`
              : "No operation is claimed now."
          }
        />
        <Summary
          tone={outcome.tone}
          title={outcome.title}
          sub={outcome.at ? relativeTo(outcome.at, now) : "No outcome recorded"}
        />
        <Summary
          tone={schema?.pendingObservation ? "unk" : "off"}
          title={schema?.pendingObservation ? "A fresh read is due" : "Next read"}
          sub={next ? `${next.toLocaleTimeString()}, ${relativeTo(next, now)}` : "None scheduled"}
        />
      </section>

      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <div className="flex min-w-0 flex-col gap-4">
          <Card title="Execution timeline">
            <StepRail steps={stages} label={`Stages of ${resource.name}`} />
            <span className="text-xs text-mute">
              {done} of {resource.stages.length} stages done
              {operation && started
                ? ` · ${operation.type} started ${started.toLocaleTimeString()}, running ${duration(now.getTime() - started.getTime())}`
                : ""}
            </span>
          </Card>

          <Card title="Job diagnostics">
            {diagnostics.isPending ? (
              <Loading what="the Job" />
            ) : diagnostics.error ? (
              <span role="alert" className="text-sm text-red">
                {ConnectError.from(diagnostics.error).rawMessage}
              </span>
            ) : jobs.length > 0 ? (
              <ul
                aria-label="Jobs the status names"
                className="m-0 flex list-none flex-col gap-4 p-0"
              >
                {jobs.map((job) => (
                  <li key={job.namedBy} className="flex flex-col gap-2">
                    <JobFacts job={job} namespace={namespace} />
                  </li>
                ))}
              </ul>
            ) : (
              <span className="text-sm text-fg2">The status names no Job now.</span>
            )}
          </Card>

          {log ? (
            <Card title={`Job log (excerpt) · ${log.where} · plain text`}>
              <Terminal label="Log excerpt" text={log.text} />
              <span className="text-xs text-mute">
                Read while the Pod exists; the console stores nothing.
                {log.truncated ? " Earlier lines are not shown." : ""}
              </span>
            </Card>
          ) : null}

          <Card title="Observed database state">
            <DatabaseState resource={resource} now={now} />
          </Card>

          <Card title="Kubernetes Events">
            <ResourceEvents response={diagnostics.data} now={now} />
          </Card>
        </div>

        <div className="flex min-w-0 flex-col gap-4">
          <Card title="What can you do next?">
            <ol className="m-0 flex flex-col gap-2 pl-5 text-[13px]">
              {attention?.nextStep ? <li>{attention.nextStep}</li> : null}
              {waitingForApproval(resource) ? (
                <li>
                  <Link to={planPage} params={{ namespace, name }} className="font-medium text-pri">
                    Review the plan
                  </Link>{" "}
                  and approve it, or leave it for another approver.
                </li>
              ) : null}
              {resource.unresolvedRun ? (
                <li>
                  Read the database&apos;s migration history, then acknowledge run{" "}
                  <span className="font-mono">{resource.unresolvedRun.operationId}</span> on{" "}
                  <Link {...resourceLink(resource)} className="font-medium text-pri">
                    the migration&apos;s page
                  </Link>
                  .
                </li>
              ) : null}
              <li>
                See every event the operator kept in{" "}
                <Link
                  to={historyPage}
                  params={{ namespace, name }}
                  className="font-medium text-pri"
                >
                  History
                </Link>
                .
              </li>
            </ol>
            {attention?.command ? (
              <CommandBlock label="Command that shows the same state" command={attention.command} />
            ) : null}
            <p className="m-0 rounded-xl bg-amberbg px-3 py-2 text-[13px] text-fg">
              No blind retry, no automatic rollback. The operator settles an outcome nobody can
              prove by reading the database, and a person decides the rest.
            </p>
          </Card>

          <Card title="Related resources">
            <RelatedResources resource={resource} />
          </Card>

          <Card title="Failure causes">
            {kept.complete ? (
              causes.length === 0 ? (
                <span className="text-sm text-fg2">
                  Nothing failed since {kept.complete.toLocaleString()}.
                </span>
              ) : (
                <ul className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
                  {causes.map((cause) => (
                    <li key={cause.cause} className="flex items-center gap-2">
                      <StatusGlyph tone="bad" size={20} />
                      <span className="flex-auto">{cause.cause}</span>
                      <span className="font-mono text-xs">{cause.count}</span>
                    </li>
                  ))}
                </ul>
              )
            ) : (
              <span className="text-sm text-fg2">
                The operator keeps no journal of this resource.
              </span>
            )}
            {kept.complete ? (
              <span className="text-xs text-mute">
                From the operator&apos;s journal of this resource, complete since{" "}
                {kept.complete.toLocaleString()}.
              </span>
            ) : null}
          </Card>
        </div>
      </div>
    </>
  )
}

function Summary({ tone, title, sub }: { tone: Tone; title: string; sub: string }) {
  return (
    <span className="flex items-start gap-2.5 rounded-2xl border border-line bg-card p-3.5 shadow-card">
      <StatusGlyph tone={tone} size={22} />
      <span className="flex min-w-0 flex-col">
        <b className="text-[13.5px] font-semibold">{title}</b>
        <span className="text-xs text-mute">{sub}</span>
      </span>
    </span>
  )
}

/** What a Job's own conditions say, then what its Pods do. */
function jobVerdict(state: JobEvidence["state"]) {
  const holds = (type: string) =>
    state?.conditions.some((condition) => condition.type === type && condition.status === "True")
  if (holds("Failed")) {
    return "Failed"
  }
  if (holds("Complete")) {
    return "Complete"
  }
  if ((state?.active ?? 0) > 0) {
    return "Running"
  }
  return (state?.failed ?? 0) > 0 ? "A Pod failed" : "No Pod running"
}

function JobFacts({ job, namespace }: { job: JobEvidence; namespace: string }) {
  if (job.evidence !== Evidence.SHOWN) {
    return (
      <span className="text-sm text-fg2">
        Job <span className="font-mono">{job.name}</span>: {goneWord(job.evidence)}
        {job.problem ? `. ${job.problem}` : ""}
      </span>
    )
  }
  const state = job.state
  const failedContainer = job.pods
    .flatMap((pod) => pod.containers.map((container) => ({ pod, container })))
    .find(({ container }) => container.state === "terminated" && container.exitCode !== 0)
  const verdict = jobVerdict(state)
  return (
    <>
      <Facts
        rows={[
          [
            "Job",
            <span key="j" className="font-mono">
              {job.name}
            </span>,
          ],
          [
            "Named by",
            <span key="n" className="font-mono text-xs">
              {job.namedBy}
            </span>,
          ],
          ["State", verdict],
          ["Started", state?.startedAt ? timestampDate(state.startedAt).toLocaleString() : "–"],
          [
            "Completed",
            state?.completedAt ? timestampDate(state.completedAt).toLocaleString() : "–",
          ],
          ...(failedContainer
            ? ([
                [
                  "Failed container",
                  `${failedContainer.container.name} in ${failedContainer.pod.name}, exit ${failedContainer.container.exitCode}${failedContainer.container.reason ? `, ${failedContainer.container.reason}` : ""}`,
                ],
              ] as [string, string][])
            : []),
        ]}
      />
      <CommandBlock
        label="See it in the cluster"
        command={`kubectl -n ${namespace} describe job ${job.name}`}
      />
    </>
  )
}

function DatabaseState({ resource, now }: { resource: ResourceView; now: Date }) {
  const schema = resource.schema
  const migration = resource.migration
  if (schema?.pendingObservation) {
    return (
      <span className="flex items-start gap-2 text-[13px]">
        <StatusGlyph tone="unk" size={20} />
        <span>
          Pending: a fresh read-only observation has not completed since Apply Job{" "}
          <span className="font-mono">{schema.pendingObservation.applyJobName}</span> ended (
          {schema.pendingObservation.outcome}). It decides what the Apply did.
        </span>
      </span>
    )
  }
  if (schema?.target?.lastObservedAt) {
    const findings = schema.target.driftFindingCount
    return (
      <span className="flex items-start gap-2 text-[13px]">
        <StatusGlyph tone={findings > 0 ? "warn" : "ok"} size={20} />
        <span>
          Read {ago(timestampDate(schema.target.lastObservedAt), now)}:{" "}
          {findings > 0
            ? `${findings} drift ${findings === 1 ? "finding" : "findings"}, highest ${schema.target.highestDriftSeverity || "unknown"}`
            : "no drift from the desired schema"}
          .
        </span>
      </span>
    )
  }
  const history = migration?.history
  if (history?.observedAt) {
    return (
      <span className="flex items-start gap-2 text-[13px]">
        <StatusGlyph tone={history.dirty ? "bad" : "ok"} size={20} />
        <span>
          History read {ago(timestampDate(history.observedAt), now)}: version{" "}
          {String(history.currentVersion)}, {history.appliedCount} applied, {history.pendingCount}{" "}
          pending{history.dirty ? ", and a dirty revision" : ""}.
        </span>
      </span>
    )
  }
  return <span className="text-sm text-fg2">The database has not been read yet.</span>
}
