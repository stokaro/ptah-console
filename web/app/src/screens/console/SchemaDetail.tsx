import { Card, Pill, StepRail } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { useResource } from "../../api/resources"
import { say, steps } from "../../model/resource"
import { Loading, ReadError, useNow } from "../common"
import { Sentence } from "../Releases"
import { GlanceTiles } from "./glance"
import { GitOpsNote, RecentObservations, schemaTiles, VerificationCard } from "./schemaGlance"
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

export interface SchemaDetailProps {
  namespace: string
  name: string
}

/**
 * One PtahSchema: where it stands and why, its current and applied plans,
 * every condition, the artifact and target it is bound to, the reconciliation
 * under way, and its spec. The page reads what the resource list reads.
 */
export function SchemaDetail({ namespace, name }: SchemaDetailProps) {
  const { resource, loading, error } = useResource("schema", namespace, name)
  const now = useNow(5_000)
  if (loading) {
    return <Loading what="the schema" />
  }
  if (error) {
    return <ReadError error={error} what="the schema" />
  }
  const schema = resource?.schema
  if (!resource || !schema) {
    return (
      <Card title="Not found">
        <span className="text-sm">
          No schema {name} in {namespace} that this identity may read.
        </span>
      </Card>
    )
  }
  const sentence = say(resource, now)
  const plan = schema.currentPlan
  const applied = schema.applied
  const operation = schema.activeOperation
  const pending = schema.pendingObservation
  const chips = [
    `namespace ${resource.namespace}`,
    `apply ${schema.applyPolicy || "OnApproval"}`,
    schema.allowDestructive ? "destructive changes allowed" : "destructive changes refused",
    `generation ${resource.ref.generation}`,
    ...(resource.gitops?.controller
      ? [
          `managed by ${resource.gitops.controller}${resource.gitops.application ? ` · ${resource.gitops.application}` : ""}`,
        ]
      : []),
  ]
  return (
    <div className="flex flex-col gap-4">
      <DetailHeader
        resource={resource}
        kind="PtahSchema"
        chips={chips}
        actions={
          plan ? (
            <Link
              to="/console/schemas/$namespace/$name/plan"
              params={{ namespace, name }}
              search={{ plan: plan.name }}
              className="inline-flex min-h-10 items-center rounded-[11px] border border-pri bg-pri px-3.5 text-[13.5px] font-semibold text-onpri"
            >
              Review plan
            </Link>
          ) : null
        }
      />
      <GlanceTiles tiles={schemaTiles(resource, now)} />
      <Card label="Where it stands">
        <StepRail steps={steps(resource)} label={`Progress of ${resource.name}`} />
        <Sentence tone={sentence.tone} text={sentence.text} large />
        <GitOpsNote resource={resource} />
      </Card>
      <AttentionCard resource={resource} />
      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Current plan">
          {plan ? (
            <Facts
              rows={[
                ["Plan", <Mono key="n" value={plan.name} />],
                ["Fingerprint", <Mono key="f" value={plan.fingerprint} short />],
                ["Planned", <When key="c" at={plan.createdAt} now={now} />],
                ["Statements", String(plan.statementCount)],
                [
                  "Findings",
                  <span key="r" className="flex flex-wrap gap-1.5">
                    {plan.destructive ? <Pill tone="bad">Destructive</Pill> : null}
                    {plan.privilegeChanges.map((change) => (
                      <Pill key={change} tone="unk">
                        {change}
                      </Pill>
                    ))}
                    {!plan.destructive && plan.privilegeChanges.length === 0 ? (
                      <span className="text-fg2">
                        No destructive statement, no privilege change
                      </span>
                    ) : null}
                  </span>,
                ],
                [
                  "Approval",
                  plan.approval ? (
                    <span key="a">
                      Approved by <b>{plan.approval.approver}</b>,{" "}
                      <When at={plan.approval.approvedAt} now={now} />
                    </span>
                  ) : (
                    <span key="a" className="text-fg2">
                      Not approved
                    </span>
                  ),
                ],
              ]}
            />
          ) : (
            <span className="text-sm text-fg2">
              No plan is published. The operator plans when the database differs from the artifact.
            </span>
          )}
        </Card>
        <Card title="Applied">
          {applied ? (
            <Facts
              rows={[
                ["Plan", <Mono key="n" value={applied.planName} />],
                ["Fingerprint", <Mono key="f" value={applied.planFingerprint} short />],
                ["Artifact", <Mono key="d" value={applied.artifactDigest} short />],
                ["Completed", <When key="c" at={applied.completedAt} now={now} />],
              ]}
            />
          ) : (
            <span className="text-sm text-fg2">No plan has been applied to this database yet.</span>
          )}
        </Card>
      </div>
      <ConditionsCard conditions={resource.conditions} now={now} />
      <div className="grid gap-4 lg:grid-cols-2">
        <ArtifactCard source={schema.source} now={now} />
        <TargetCard target={schema.target} now={now} />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <RecentObservations resource={resource} now={now} />
        <VerificationCard resource={resource} now={now} />
      </div>
      <Card title="Reconciliation">
        <Facts
          rows={[
            [
              "Running",
              operation ? (
                <span key="o">
                  {operation.type} <Mono value={operation.id} />, Job{" "}
                  <code className="font-mono text-xs">{operation.jobName}</code>, attempt{" "}
                  {operation.attempt}, started <When at={operation.startedAt} now={now} />
                </span>
              ) : (
                <span key="o" className="text-fg2">
                  No operation
                </span>
              ),
            ],
            [
              "Verifying",
              pending ? (
                <span key="p">
                  {pending.outcome}, Apply Job{" "}
                  <code className="font-mono text-xs">{pending.applyJobName}</code>, read again{" "}
                  <When at={pending.observeAfter} now={now} />
                </span>
              ) : (
                <span key="p" className="text-fg2">
                  Nothing waits for a fresh read
                </span>
              ),
            ],
            ["Last attempt", <When key="a" at={schema.lastAttempt} now={now} />],
            ["Last success", <When key="s" at={schema.lastSuccessfulReconciliation} now={now} />],
            ["Next read", <When key="n" at={schema.nextReconciliation} now={now} />],
          ]}
        />
      </Card>
      <SpecCard resource={resource} spec={schema.specYaml} />
    </div>
  )
}
