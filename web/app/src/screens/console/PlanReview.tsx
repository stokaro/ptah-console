import { skipToken, useQuery } from "@connectrpc/connect-query"
import {
  BindingTable,
  Button,
  Card,
  Eyebrow,
  FindingList,
  Fingerprint,
  Pill,
  Segmented,
  SqlView,
  StatusGlyph,
} from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useState } from "react"

import { useDecisions, useOffer, useResource, useSession } from "../../api/resources"
import { DecisionState } from "../../gen/ptah/console/v1/decisions_pb"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import {
  MigrationService,
  PlanSelection,
  SchemaService,
  SqlAccess,
  type SchemaPlan,
} from "../../gen/ptah/console/v1/resources_pb"
import { decisionState } from "../../model/decision"
import {
  approvalChecks,
  filterStatements,
  integrity,
  statementTypes,
  tombstone,
  type Tombstone,
} from "../../model/planreview"
import {
  planRisks,
  statementTag,
  waitingForApproval,
  type ResourceKind,
} from "../../model/resource"
import { ApproveDialog, type ReviewedPlan } from "../ApproveDialog"
import { Loading, ReadError } from "../common"
import { bindings, MigrationSteps } from "../PlanReview"
import { resourceLink } from "./links"

const docs = "https://operator.ptah.run/reference/reconciliation/"

export interface ConsolePlanReviewProps {
  kind: ResourceKind
  namespace: string
  name: string
  /** The plan the link that opened the page named, if it named one. */
  followed?: string
  /** Drops the plan the link named and reads the current one. */
  onReadCurrent: () => void
}

const nextSteps = [
  "The approval is admitted only if it names the current plan, by UID and fingerprint, and admission stamps who sent it.",
  "At its next pass the operator checks the plan and the approval again, then claims the Apply under the database's lock.",
  "A Job runs exactly the approved plan, and nothing else.",
  "A fresh read-only observation proves the result; the Job's exit code proves nothing.",
]

/**
 * The full console's review of a resource's current plan: what it changes,
 * its exact SQL with the plan's own marks, whether its stored chunks checked
 * out, what an approval binds to, and the checks the console runs before it
 * offers one. The operator, not the console, has the final word.
 */
export function ConsolePlanReview({
  kind,
  namespace,
  name,
  followed,
  onReadCurrent,
}: ConsolePlanReviewProps) {
  const { resource, loading, error } = useResource(kind, namespace, name)
  const session = useSession()
  const offer = useOffer()
  const { decisions } = useDecisions()
  const [approving, setApproving] = useState<ReviewedPlan>()
  const [readUid, setReadUid] = useState<string>()
  const [filter, setFilter] = useState("")
  const [markedOnly, setMarkedOnly] = useState(false)
  const hasPlan = Boolean(resource?.plan)
  const schemaPlan = useQuery(
    SchemaService.method.getSchemaPlan,
    kind === "schema" && hasPlan
      ? { namespace, schema: name, selection: PlanSelection.CURRENT }
      : skipToken
  )
  const migrationPlan = useQuery(
    MigrationService.method.getMigrationPlan,
    kind === "migration" && hasPlan ? { namespace, migration: name } : skipToken
  )
  const shown = schemaPlan.data?.plan ?? migrationPlan.data?.plan
  if (!readUid && shown?.uid) {
    setReadUid(shown.uid)
  }
  const replaced = Boolean(readUid && shown?.uid && shown.uid !== readUid)

  if (loading) {
    return <Loading what="the plan" />
  }
  if (error) {
    return <ReadError error={error} what="the plan" />
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

  const waiting = waitingForApproval(resource)
  const allowed = offer(
    namespace,
    kind === "schema" ? "approveSchemaPlans" : "approveMigrationPlans"
  )
  const sPlan = schemaPlan.data?.plan
  const mPlan = migrationPlan.data?.plan
  const removed = tombstone(resource, followed, decisions.data?.decisions ?? [])
  const checks = approvalChecks({
    current: !replaced && !removed,
    contractSupported: kind === "schema" ? (sPlan?.contractSupported ?? true) : undefined,
    allowed,
    namespace,
  })
  const planError = kind === "schema" ? schemaPlan.error : migrationPlan.error
  const identity = session.isSuccess ? session.data : undefined

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
          › plan
        </nav>
        <Eyebrow>{kind === "schema" ? "Schema plan" : "Migration plan"}</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Plan review
        </h1>
        <span className="text-[13.5px] text-fg2">
          Inspect the exact change, what the plan says about it, and the state an approval binds to.
        </span>
        <ul aria-label="About" className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0">
          {resource.plan ? (
            <li>
              <Pill>{resource.plan.name}</Pill>
            </li>
          ) : null}
          <li>
            <Pill tone={waiting ? "warn" : "off"}>
              {waiting ? "Approval required" : "Not waiting for approval"}
            </Pill>
          </li>
          <li>
            <Link {...resourceLink(resource)} className="text-xs font-medium text-pri">
              View the {kind}
            </Link>
          </li>
        </ul>
      </section>

      {removed ? <RemovedPlan tomb={removed} onReadCurrent={onReadCurrent} /> : null}
      {replaced ? (
        <Card title="The plan changed">
          <span role="alert" className="text-sm leading-normal">
            A newer plan replaced the one you were reading. This page now shows the new plan; read
            it before you approve anything.
          </span>
          <Button className="self-start" onClick={() => setReadUid(shown?.uid)}>
            I am reading the new plan
          </Button>
        </Card>
      ) : null}

      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1.8fr)_minmax(300px,1fr)]">
        <div className="flex min-w-0 flex-col gap-4">
          <Card title="What this changes">
            <FindingList findings={planRisks(resource, sPlan)} />
            <span className="text-xs text-mute">
              From the plan&apos;s manifest, as Ptah recorded it, not from reading the SQL.
            </span>
          </Card>
          {planError ? <ReadError error={planError} what="the plan" /> : null}
          {sPlan ? (
            <SchemaPlanParts
              plan={sPlan}
              filter={filter}
              onFilter={setFilter}
              markedOnly={markedOnly}
              onMarkedOnly={setMarkedOnly}
            />
          ) : null}
          {mPlan ? <MigrationSteps plan={mPlan} /> : null}
          {sPlan || mPlan ? (
            <Card title="Bound to">
              <BindingTable rows={bindings(resource, sPlan, mPlan)} />
              <Fingerprint value={(sPlan ?? mPlan)?.fingerprint ?? ""} />
            </Card>
          ) : schemaPlan.isLoading || migrationPlan.isLoading ? (
            <Loading what="the plan" />
          ) : !hasPlan ? (
            <Card title="The plan">
              <span className="text-sm leading-normal">
                No plan is current: the operator is planning again, as it does each interval, or
                there is nothing to plan. A plan appears here once it is published.
              </span>
            </Card>
          ) : null}
        </div>

        <section
          aria-label="Approval"
          className="flex flex-col gap-3 rounded-[18px] border border-line bg-card p-[18px] shadow-card lg:sticky lg:top-[76px]"
        >
          <span className="flex items-center gap-2">
            <StatusGlyph tone={waiting ? "warn" : "off"} size={22} />
            <b className="text-[15px] font-semibold">
              {waiting ? "Approval required" : "Nothing to approve"}
            </b>
          </span>
          <span className="text-[13px] text-fg2">
            {waiting
              ? "This plan is applied only after a person approves exactly this plan."
              : "The resource is not waiting for an approval now."}
          </span>
          {identity ? (
            <span className="text-xs text-mute">
              Approving as{" "}
              <b className="font-mono font-semibold text-fg">{identity.kubernetesUsername}</b>,{" "}
              {identity.mode === Mode.LOCAL ? "from the kubeconfig" : "a Console account"};
              admission records who.
            </span>
          ) : null}
          <ul
            aria-label="Checks before approving"
            className="m-0 flex list-none flex-col gap-2 p-0"
          >
            {checks.map((check) => (
              <li key={check.label} className="flex items-start gap-2 text-[13px]">
                <StatusGlyph tone={check.tone} size={20} />
                <span className="flex flex-col">
                  {check.label}
                  {check.note ? <span className="text-xs text-mute">{check.note}</span> : null}
                </span>
              </li>
            ))}
          </ul>
          {waiting ? (
            <Button
              variant="primary"
              disabled={!shown || checks.some((check) => check.tone !== "ok")}
              onClick={() =>
                shown &&
                setApproving({
                  ownerUid: resource.ref.uid,
                  name: shown.name,
                  uid: shown.uid,
                  fingerprint: shown.fingerprint,
                  contractVersion: shown.contractVersion,
                })
              }
            >
              Approve this plan…
            </Button>
          ) : null}
          <span className="text-xs text-mute">
            The checks run against the cluster now. The operator, not the console, has the final
            word, and checks the plan and the approval again before it runs anything.
          </span>
          <details className="text-[13px]">
            <summary className="cursor-pointer font-medium">What happens next</summary>
            <ol className="m-0 mt-2 flex flex-col gap-1.5 pl-5 text-fg2">
              {nextSteps.map((step) => (
                <li key={step}>{step}</li>
              ))}
            </ol>
          </details>
          <details className="text-[13px]">
            <summary className="cursor-pointer font-medium">Why approvals can become stale</summary>
            <p className="m-0 mt-2 text-fg2">
              An approval names one plan by UID and fingerprint. If the desired state, the database,
              a policy or the executor changes, the operator plans again, and the old approval no
              longer matches anything it would run. Approving one plan can never apply another.{" "}
              <a href={docs} className="font-medium text-pri">
                Reconciliation
              </a>
            </p>
          </details>
        </section>
      </div>

      {approving ? (
        <ApproveDialog
          resource={resource}
          context={identity?.context}
          reviewed={approving}
          pages="console"
          onClose={() => setApproving(undefined)}
        />
      ) : null}
    </>
  )
}

function RemovedPlan({ tomb, onReadCurrent }: { tomb: Tombstone; onReadCurrent: () => void }) {
  return (
    <section
      aria-label="Plan no longer current"
      className="flex flex-col gap-2 rounded-[18px] border border-dashed border-line2 bg-card p-[18px]"
    >
      <span className="flex items-center gap-2">
        <StatusGlyph tone="off" size={22} />
        <b className="text-[15px] font-semibold">
          Plan <span className="font-mono">{tomb.name}</span> is no longer the current plan
        </b>
      </span>
      <span className="text-[13px] text-fg2">
        {tomb.current ? (
          <>
            <span className="font-mono">{tomb.current}</span> replaced it.{" "}
          </>
        ) : (
          "No plan is current now. "
        )}
        A plan is immutable while the operator holds it, and an approval that named this one cannot
        be used for another.
      </span>
      <ul className="m-0 flex list-none flex-col gap-1 p-0 text-[13px] text-fg2">
        {tomb.published ? <li>Published {tomb.published.toLocaleString()}</li> : null}
        {tomb.applied ? <li>Applied {tomb.applied.toLocaleString()}</li> : null}
        {tomb.approvals.map((approval) => (
          <li key={approval.name}>
            Approval <span className="font-mono">{approval.name}</span>
            {approval.by ? ` by ${approval.by}` : ""}: {decisionState(approval.state).label}
            {approval.state === DecisionState.STALE ? ", and it will not be used" : ""}
          </li>
        ))}
        {!tomb.published && !tomb.applied && tomb.approvals.length === 0 ? (
          <li>
            Neither the resource&apos;s journal nor the decisions the console reads mention it.
          </li>
        ) : null}
      </ul>
      {tomb.current ? (
        <Button className="self-start" onClick={onReadCurrent}>
          Read the current plan
        </Button>
      ) : null}
    </section>
  )
}

function SchemaPlanParts({
  plan,
  filter,
  onFilter,
  markedOnly,
  onMarkedOnly,
}: {
  plan: SchemaPlan
  filter: string
  onFilter: (text: string) => void
  markedOnly: boolean
  onMarkedOnly: (marked: boolean) => void
}) {
  const checked = integrity(plan)
  const available = plan.sqlAccess === SqlAccess.AVAILABLE
  const types = statementTypes(plan.statements)
  const shown = filterStatements(plan.statements, filter, markedOnly)
  return (
    <>
      <div className="grid gap-4 md:grid-cols-2">
        <Card title="Statement types">
          {available ? (
            <ul className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
              {types.marked.map((type) => (
                <li key={type.tag} className="flex items-center gap-2">
                  <StatusGlyph tone={type.tone ?? "off"} size={20} />
                  <span className="flex-auto">{type.tag}</span>
                  <span className="font-mono text-xs">{type.count}</span>
                </li>
              ))}
              <li className="flex items-center gap-2 text-fg2">
                <StatusGlyph tone="ok" size={20} />
                <span className="flex-auto">No mark</span>
                <span className="font-mono text-xs">{types.unmarked}</span>
              </li>
            </ul>
          ) : (
            <span className="text-sm text-fg2">Not available: the SQL is not shown.</span>
          )}
          <span className="text-xs text-mute">
            The marks are the plan&apos;s own, from Ptah&apos;s severity and reason for each
            statement; the console does not parse SQL.
          </span>
        </Card>
        <Card title="Integrity">
          <span className="flex items-center gap-2 text-[13px] font-semibold">
            <StatusGlyph tone={checked.tone} size={20} />
            {checked.title}
          </span>
          {checked.lines.map((line) => (
            <span key={line} className="text-[13px] text-fg2">
              {line}
            </span>
          ))}
        </Card>
      </div>
      {available ? (
        <Card title="Exact SQL · plain text">
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex h-9 min-w-[220px] flex-auto items-center gap-2 rounded-[10px] border border-line bg-bg px-2.5 text-mute">
              <span aria-hidden="true">⌕</span>
              <input
                value={filter}
                onChange={(event) => onFilter(event.target.value)}
                placeholder="Filter the statements"
                aria-label="Filter the statements"
                className="min-w-0 flex-auto border-0 bg-transparent font-mono text-sm text-fg outline-0"
              />
            </label>
            <Segmented
              label="Statements"
              value={markedOnly ? "marked" : "all"}
              onChange={(value) => onMarkedOnly(value === "marked")}
              options={[
                { value: "all", label: "All", count: plan.statements.length },
                {
                  value: "marked",
                  label: "Marked",
                  count: plan.statements.length - types.unmarked,
                },
              ]}
            />
          </div>
          {shown.length === 0 ? (
            <p className="m-0 text-sm text-fg2">No statement matches.</p>
          ) : (
            <SqlView
              lines={shown.map(({ statement, number }) => {
                const tag = statementTag(statement)
                return { number, sql: statement.sql, tag: tag.tag, tagTone: tag.tone }
              })}
            />
          )}
          <span role="status" className="text-xs text-mute">
            {shown.length === plan.statements.length
              ? `${plan.statements.length} statements, numbered as the plan runs them.`
              : `Showing ${shown.length} of ${plan.statements.length} statements, numbered as the plan runs them.`}
          </span>
        </Card>
      ) : (
        <Card title="Exact SQL">
          <span className="text-sm text-fg2">{plan.sqlProblem || "The SQL is not shown."}</span>
        </Card>
      )}
    </>
  )
}
