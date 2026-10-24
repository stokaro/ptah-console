import type { Timestamp } from "@bufbuild/protobuf/wkt"
import { ConnectError } from "@connectrpc/connect"
import { skipToken, useQuery } from "@connectrpc/connect-query"
import { Button, Card, Pill, StatusGlyph, StepRail, Tabs, type Tone } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useState, type ReactNode } from "react"

import { offerText, useOffer, useResource, useSession, type Offer } from "../../api/resources"
import { ResourceKind } from "../../gen/ptah/console/v1/decisions_pb"
import { DiagnosticsService } from "../../gen/ptah/console/v1/diagnostics_pb"
import {
  AttentionKind,
  MigrationService,
  type MigrationHistory,
  type MigrationRun,
  type Operation,
  type ResolvedRun,
  type UnresolvedRun,
} from "../../gen/ptah/console/v1/resources_pb"
import { whereStopped } from "../../model/activity"
import { say, steps, waitingForApproval, type ResourceView } from "../../model/resource"
import { migrationTimeline, unlistedApplied } from "../../model/timeline"
import { Loading, ReadError, useNow } from "../common"
import { AcknowledgeDialog } from "../IncidentDetail"
import { Sentence } from "../Releases"
import { MigrationGlance, RunsCard } from "./MigrationRuns"
import { ExecutionTimesCard, RevisionHistoryCard, TimelineCard } from "./MigrationTimeline"
import {
  ArtifactCard,
  AttentionCard,
  ConditionsCard,
  DetailHeader,
  Facts,
  Mono,
  SpecCard,
  TargetCard,
  When,
} from "./detail"
import { RelatedResources, ResourceEvents } from "./evidence"
import type { MigrationTab } from "./migrationTabs"

export interface MigrationDetailProps {
  namespace: string
  name: string
  /** The panel that is open; the address keeps it. */
  tab: MigrationTab
  onTab: (tab: MigrationTab) => void
}

/**
 * One PtahMigration. A migration does not converge, it advances: the page
 * shows how far the history is, what the plan would apply next, how the last
 * run ended, and what a person may do about it.
 */
export function MigrationDetail({ namespace, name, tab, onTab }: MigrationDetailProps) {
  const { resource, loading, error } = useResource("migration", namespace, name)
  const session = useSession()
  const offer = useOffer()
  const now = useNow(5_000)
  // Held from the moment the dialog opens: the operator settles the run
  // seconds after the acknowledgment, and the dialog still reports the end.
  const [acknowledging, setAcknowledging] = useState<UnresolvedRun>()
  const migration = resource?.migration
  const plan = useQuery(
    MigrationService.method.getMigrationPlan,
    migration?.planName ? { namespace, migration: name } : skipToken
  )
  if (loading) {
    return <Loading what="the migration" />
  }
  if (error) {
    return <ReadError error={error} what="the migration" />
  }
  if (!resource || !migration) {
    return (
      <Card title="Not found">
        <span className="text-sm">
          No migration {name} in {namespace} that this identity may read.
        </span>
      </Card>
    )
  }
  const sentence = say(resource, now)
  const history = migration.history
  const run = migration.lastRun
  const unresolved = migration.unresolvedRun
  const resolved = migration.resolvedRun
  const operation = migration.activeOperation
  const waiting = waitingForApproval(resource)
  const acknowledge = offer(namespace, "acknowledgeRuns")
  const timeline = migrationTimeline(migration, plan.data?.plan)
  const chips = [
    `namespace ${namespace}`,
    ...(migration.target?.engine ? [migration.target.engine] : []),
    `apply ${migration.applyPolicy || "OnApproval"}`,
    `generation ${resource.ref.generation}`,
    ...(resource.gitops?.controller ? [`managed by ${resource.gitops.controller}`] : []),
  ]
  return (
    <div className="flex flex-col gap-4">
      <DetailHeader
        resource={resource}
        kind="PtahMigration"
        chips={chips}
        actions={
          migration.planName ? (
            <Link
              to="/console/migrations/$namespace/$name/plan"
              params={{ namespace, name }}
              search={{ plan: migration.planName }}
              className={`inline-flex min-h-10 items-center rounded-[11px] border px-3.5 text-[13.5px] font-semibold ${waiting ? "border-pri bg-pri text-onpri" : "border-line bg-card text-fg"}`}
            >
              Review plan
            </Link>
          ) : null
        }
      />
      <MigrationGlance resource={resource} />
      <Card label="Where it stands">
        <StepRail steps={steps(resource)} label={`Progress of ${resource.name}`} />
        <Sentence tone={sentence.tone} text={sentence.text} large />
      </Card>
      <AttentionCard resource={resource} />
      <Tabs
        label={`Sections of ${resource.name}`}
        value={tab}
        onChange={onTab}
        tabs={[
          { value: "history", label: "History" },
          { value: "events", label: "Events" },
          { value: "configuration", label: "Configuration" },
          { value: "related", label: "Related" },
        ]}
      >
        {tab === "history" ? (
          <>
            <div className="grid gap-4 lg:grid-cols-2">
              <LastRunCard run={run} now={now} />
              <SupportedActions
                namespace={namespace}
                unresolved={unresolved}
                resolved={resolved}
                acknowledge={acknowledge}
                waiting={Boolean(waiting)}
                failedStep={
                  resource.attention.find((a) => a.kind === AttentionKind.FAILED)?.nextStep
                }
                now={now}
                onAcknowledge={setAcknowledging}
              />
            </div>
            {plan.error ? (
              <span className="text-sm text-fg2">
                The plan is not shown: {ConnectError.from(plan.error).rawMessage}
              </span>
            ) : null}
            <TimelineCard
              name={resource.name}
              steps={timeline}
              unlisted={unlistedApplied(migration)}
              now={now}
            />
            <div className="grid gap-4 lg:grid-cols-2">
              <HistoryCard history={history} next={migration.nextReconciliation} now={now} />
              <RunsCard resource={resource} now={now} />
            </div>
            <RevisionHistoryCard
              name={resource.name}
              revisions={history?.revisions ?? []}
              now={now}
            />
            <ExecutionTimesCard revisions={history?.revisions ?? []} />
            {operation ? <RunningCard operation={operation} now={now} /> : null}
          </>
        ) : tab === "events" ? (
          <MigrationEvents resource={resource} now={now} />
        ) : tab === "configuration" ? (
          <>
            <ConditionsCard conditions={resource.conditions} now={now} />
            <div className="grid gap-4 lg:grid-cols-2">
              <ArtifactCard source={migration.source} now={now} />
              <TargetCard target={migration.target} now={now} />
            </div>
            <SpecCard resource={resource} spec={migration.specYaml} />
          </>
        ) : (
          <Card title="Related resources">
            <RelatedResources resource={resource} />
          </Card>
        )}
      </Tabs>
      {acknowledging ? (
        <AcknowledgeDialog
          resource={resource}
          run={acknowledging}
          context={session.data?.context}
          onClose={() => setAcknowledging(undefined)}
        />
      ) : null}
    </div>
  )
}

function HistoryCard({
  history,
  next,
  now,
}: {
  history?: MigrationHistory
  next?: Timestamp
  now: Date
}) {
  return (
    <Card title="History">
      {history ? (
        <Facts
          rows={[
            [
              "Current version",
              <span key="c" className="font-mono">
                {String(history.currentVersion)}
              </span>,
            ],
            [
              "Checkpoint",
              history.checkpointVersion ? (
                <span key="k" className="font-mono">
                  {String(history.checkpointVersion)}
                </span>
              ) : (
                <span key="k" className="text-mute">
                  none
                </span>
              ),
            ],
            ["Applied", `${history.appliedCount} of the artifact's migrations`],
            ["Pending", String(history.pendingCount)],
            [
              "Revision table",
              history.dirty ? (
                <span key="d" className="inline-flex items-center gap-1.5">
                  <StatusGlyph tone="bad" size={20} />A failed or interrupted run left a row;
                  nothing applies until it is repaired.
                </span>
              ) : (
                <span key="d" className="inline-flex items-center gap-1.5">
                  <StatusGlyph tone="ok" size={20} />
                  Clean
                </span>
              ),
            ],
            ["Read", <When key="r" at={history.observedAt} now={now} />],
            ["Next read", <When key="n" at={next} now={now} />],
          ]}
        />
      ) : (
        <span className="text-sm text-fg2">The operator has not read the history yet.</span>
      )}
      <span className="text-xs text-mute">
        The operator reads the history at its interval. After the database changed outside it,
        &ldquo;Read again now&rdquo; asks for that read sooner.
      </span>
    </Card>
  )
}

function LastRunCard({ run, now }: { run?: MigrationRun; now: Date }) {
  return (
    <Card title="Last run">
      {run ? (
        <Facts
          rows={[
            [
              "Outcome",
              <Pill key="o" tone={runTone(run.outcome)}>
                {run.outcome}
              </Pill>,
            ],
            ["What happened", run.message || <span className="text-mute">–</span>],
            ...(run.failure
              ? ([["Stopped at", whereStopped(run.failure)]] as [string, ReactNode][])
              : []),
            [
              "Applied",
              run.appliedVersions.length > 0 ? (
                <span key="a" className="font-mono">
                  {run.appliedVersions.map(String).join(", ")}
                </span>
              ) : (
                <span key="a" className="text-fg2">
                  nothing
                </span>
              ),
            ],
            ["Job", <Mono key="j" value={run.jobName} />],
            ["Started", <When key="s" at={run.startedAt} now={now} />],
            ["Finished", <When key="f" at={run.finishedAt} now={now} />],
          ]}
        />
      ) : (
        <span className="text-sm text-fg2">No run has been recorded.</span>
      )}
    </Card>
  )
}

function SupportedActions({
  namespace,
  unresolved,
  resolved,
  acknowledge,
  waiting,
  failedStep,
  now,
  onAcknowledge,
}: {
  namespace: string
  unresolved?: UnresolvedRun
  resolved?: ResolvedRun
  acknowledge: Offer
  waiting: boolean
  /** The next step the operator supports after a failed run, when one failed. */
  failedStep?: string
  now: Date
  onAcknowledge: (run: UnresolvedRun) => void
}) {
  return (
    <Card title="Supported actions">
      <ul className="m-0 flex list-none flex-col gap-2.5 p-0 text-sm">
        {unresolved ? (
          <li className="flex flex-col gap-1.5">
            <span>
              Run <Mono value={unresolved.operationId} /> ended {unresolved.outcome.toLowerCase()}{" "}
              and nothing runs until a person settles it. Read the database&apos;s migration history
              first, then acknowledge the run.
            </span>
            {acknowledge === "allowed" ? (
              <Button
                variant="attention"
                className="self-start"
                onClick={() => onAcknowledge(unresolved)}
              >
                Acknowledge this run…
              </Button>
            ) : (
              <span className="text-[13px] text-fg2">
                {offerText(acknowledge, `This identity may not acknowledge runs in ${namespace}.`)}
              </span>
            )}
          </li>
        ) : null}
        {waiting ? <li>Review the plan and approve it, from the plan review.</li> : null}
        {failedStep ? <li>{failedStep}</li> : null}
        <li className="text-fg2">
          Retry, rollback and force are not offered: the operator has no such action. A failed
          migration is fixed in the database or the artifact, and the next history read picks it up;
          &ldquo;Read again now&rdquo; asks for that read without waiting for the interval.
        </li>
      </ul>
      {resolved ? (
        <span className="text-xs text-mute">
          The last unresolved run, {resolved.operationId}, was settled ({resolved.resolution}
          {resolved.acknowledgedBy ? ` by ${resolved.acknowledgedBy}` : ""}),{" "}
          <When at={resolved.resolvedAt} now={now} />.
        </span>
      ) : null}
    </Card>
  )
}

function RunningCard({ operation, now }: { operation: Operation; now: Date }) {
  return (
    <Card title="Running">
      <Facts
        rows={[
          [
            "Operation",
            <span key="o">
              {operation.type} <Mono value={operation.id} />
            </span>,
          ],
          ["Job", <Mono key="j" value={operation.jobName} />],
          ["Attempt", String(operation.attempt)],
          ["Started", <When key="s" at={operation.startedAt} now={now} />],
        ]}
      />
    </Card>
  )
}

/** The Kubernetes Events about the migration, read when the panel opens. */
function MigrationEvents({ resource, now }: { resource: ResourceView; now: Date }) {
  const diagnostics = useQuery(DiagnosticsService.method.getDiagnostics, {
    kind: ResourceKind.MIGRATION,
    namespace: resource.namespace,
    name: resource.name,
    uid: resource.ref.uid,
    includeLogs: false,
  })
  return (
    <Card title="Kubernetes Events">
      {diagnostics.error ? (
        <ReadError error={diagnostics.error} what="the Events" />
      ) : (
        <ResourceEvents response={diagnostics.data} now={now} filters />
      )}
    </Card>
  )
}

function runTone(outcome: string): Tone {
  switch (outcome) {
    case "Applied":
    case "UpToDate":
      return "ok"
    case "Failed":
      return "bad"
    case "Partial":
    case "Unknown":
      return "unk"
    default:
      return "off"
  }
}
