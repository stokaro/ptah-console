import { ConnectError } from "@connectrpc/connect"
import { skipToken, useQuery } from "@connectrpc/connect-query"
import { Button, Card, Pill, StatusGlyph, StepRail, type Tone } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useState, type ReactNode } from "react"

import { offerText, useOffer, useResource, useSession } from "../../api/resources"
import {
  AttentionKind,
  MigrationService,
  type MigrationPlan,
  type UnresolvedRun,
} from "../../gen/ptah/console/v1/resources_pb"
import { whereStopped } from "../../model/activity"
import { say, steps, waitingForApproval } from "../../model/resource"
import { Loading, ReadError, useNow } from "../common"
import { AcknowledgeDialog } from "../IncidentDetail"
import { Sentence } from "../Releases"
import { MigrationGlance, RunsCard } from "./MigrationRuns"
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

export interface MigrationDetailProps {
  namespace: string
  name: string
}

/** One row of the migration's sequence, as the page can account for it. */
interface Step {
  version: string
  what: string
  tone: Tone
  state: string
}

/**
 * The steps the page can name: the versions the last run applied, the files
 * the current plan would apply, and the versions the history refuses. The
 * operator publishes a summary of the revision table, not its rows, so the
 * versions applied before the last run are a count, not a list.
 */
function sequence(
  appliedByLastRun: bigint[],
  plan: MigrationPlan | undefined,
  dirty: boolean,
  modified: bigint[],
  outOfOrder: bigint[]
): Step[] {
  const rows: Step[] = []
  for (const version of appliedByLastRun) {
    rows.push({
      version: String(version),
      what: "Applied by the last run",
      tone: "ok",
      state: "applied",
    })
  }
  for (const file of plan?.migrations ?? []) {
    rows.push({
      version: file.versionKey || String(file.version),
      what: `${file.description}${file.checkpoint ? " (checkpoint)" : ""}${file.transactionMode ? ` · ${file.transactionMode}` : ""}`,
      tone: dirty ? "off" : "warn",
      state: dirty ? "held: the history is dirty" : "pending",
    })
  }
  for (const version of modified) {
    rows.push({
      version: String(version),
      what: "Applied, and its file no longer accounts for it",
      tone: "bad",
      state: "modified",
    })
  }
  for (const version of outOfOrder) {
    rows.push({
      version: String(version),
      what: "Arrived below a version the database already applied",
      tone: "warn",
      state: "out of order",
    })
  }
  return rows
}

/**
 * One PtahMigration. A migration does not converge, it advances: the page
 * shows how far the history is, what the plan would apply next, how the last
 * run ended, and what a person may do about it.
 */
export function MigrationDetail({ namespace, name }: MigrationDetailProps) {
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
  const rows = sequence(
    run?.appliedVersions ?? [],
    plan.data?.plan,
    history?.dirty ?? false,
    history?.modifiedVersions ?? [],
    history?.outOfOrderVersions ?? []
  )
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
      <div className="grid gap-4 lg:grid-cols-2">
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
                ["Next read", <When key="n" at={migration.nextReconciliation} now={now} />],
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
      </div>
      <RunsCard resource={resource} now={now} />
      <Card title="Steps">
        {plan.error ? (
          <span className="text-sm text-fg2">
            The plan is not shown: {ConnectError.from(plan.error).rawMessage}
          </span>
        ) : null}
        {rows.length === 0 ? (
          <span className="text-sm text-fg2">
            {history
              ? `Nothing pending: the history stands at version ${history.currentVersion}.`
              : "Nothing to show until the operator reads the history."}
          </span>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-left text-[13px]">
              <caption className="sr-only">Steps of {resource.name}</caption>
              <thead>
                <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                  <th scope="col" className="py-2 pr-3 font-medium">
                    Version
                  </th>
                  <th scope="col" className="px-3 py-2 font-medium">
                    Step
                  </th>
                  <th scope="col" className="py-2 pl-3 font-medium">
                    State
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr
                    key={`${row.state}/${row.version}`}
                    className="border-b border-line last:border-b-0"
                  >
                    <th scope="row" className="py-2 pr-3 text-left font-mono font-medium">
                      {row.version}
                    </th>
                    <td className="px-3 py-2 text-fg2">{row.what}</td>
                    <td className="py-2 pl-3">
                      <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                        <StatusGlyph tone={row.tone} size={20} />
                        {row.state}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {history && history.currentVersion > 0n ? (
          <span className="text-xs text-mute">
            The versions applied before the last run are not listed: the operator publishes how far
            the history is, version {String(history.currentVersion)}, not each row.
          </span>
        ) : null}
      </Card>
      <Card title="Supported actions">
        <ul className="m-0 flex list-none flex-col gap-2.5 p-0 text-sm">
          {unresolved ? (
            <li className="flex flex-col gap-1.5">
              <span>
                Run <Mono value={unresolved.operationId} /> ended {unresolved.outcome.toLowerCase()}{" "}
                and nothing runs until a person settles it. Read the database&apos;s migration
                history first, then acknowledge the run.
              </span>
              {acknowledge === "allowed" ? (
                <Button
                  variant="attention"
                  className="self-start"
                  onClick={() => setAcknowledging(unresolved)}
                >
                  Acknowledge this run…
                </Button>
              ) : (
                <span className="text-[13px] text-fg2">
                  {offerText(
                    acknowledge,
                    `This identity may not acknowledge runs in ${namespace}.`
                  )}
                </span>
              )}
            </li>
          ) : null}
          {waiting ? <li>Review the plan and approve it, from the plan review.</li> : null}
          {resource.attention.some((a) => a.kind === AttentionKind.FAILED) ? (
            <li>{resource.attention.find((a) => a.kind === AttentionKind.FAILED)?.nextStep}</li>
          ) : null}
          <li className="text-fg2">
            Retry, rollback and force are not offered: the operator has no such action. A failed
            migration is fixed in the database or the artifact, and the next history read picks it
            up; &ldquo;Read again now&rdquo; asks for that read without waiting for the interval.
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
      {operation ? (
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
      ) : null}
      <ConditionsCard conditions={resource.conditions} now={now} />
      <div className="grid gap-4 lg:grid-cols-2">
        <ArtifactCard source={migration.source} now={now} />
        <TargetCard target={migration.target} now={now} />
      </div>
      <SpecCard resource={resource} spec={migration.specYaml} />
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
