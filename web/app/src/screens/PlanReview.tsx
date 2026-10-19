import { skipToken, useQuery } from "@connectrpc/connect-query"
import { timestampDate } from "@bufbuild/protobuf/wkt"
import {
  BindingTable,
  Button,
  Card,
  RiskList,
  SqlView,
  shortDigest,
  type Binding,
} from "@ptah/console-ui"
import { getRouteApi, Link } from "@tanstack/react-router"
import { useState } from "react"

import { readOnlyText, useOffer, useResources, useSession } from "../api/resources"
import {
  MigrationService,
  PlanSelection,
  SchemaService,
  SqlAccess,
  type MigrationPlan,
  type SchemaPlan,
} from "../gen/ptah/console/v1/resources_pb"
import {
  parseKind,
  planRisks,
  statementTag,
  waitingForApproval,
  type ResourceView,
} from "../model/resource"
import { planSummary } from "./ApproverQueue"
import { ApproveDialog } from "./ApproveDialog"
import { BackButton, DetailHeading, Loading, ReadError, where } from "./common"

const route = getRouteApi("/approve/$namespace/$kind/$name")

export function PlanReview() {
  const params = route.useParams()
  const kind = parseKind(params.kind)
  const { resources, loading, error } = useResources()
  const session = useSession()
  const offer = useOffer()
  const [approving, setApproving] = useState(false)
  const resource = resources.find(
    (r) => r.kind === kind && r.namespace === params.namespace && r.name === params.name
  )
  const schemaPlan = useQuery(
    SchemaService.method.getSchemaPlan,
    kind === "schema"
      ? { namespace: params.namespace, schema: params.name, selection: PlanSelection.CURRENT }
      : skipToken
  )
  const migrationPlan = useQuery(
    MigrationService.method.getMigrationPlan,
    kind === "migration" ? { namespace: params.namespace, migration: params.name } : skipToken
  )

  if (loading) {
    return <Loading what="the plan" />
  }
  if (error) {
    return <ReadError error={error} what="the plan" />
  }
  if (!resource || !kind) {
    return (
      <>
        <BackButton />
        <Card title="Not found">
          <span className="text-sm">
            No {params.kind} {params.name} in {params.namespace} that this identity may read.
          </span>
        </Card>
      </>
    )
  }

  const context = session.data?.context
  const waiting = waitingForApproval(resource)
  const mayApprove = offer(
    resource.namespace,
    kind === "schema" ? "approveSchemaPlans" : "approveMigrationPlans"
  )
  const planError = kind === "schema" ? schemaPlan.error : migrationPlan.error
  const sPlan = schemaPlan.data?.plan
  const mPlan = migrationPlan.data?.plan
  const planName = sPlan?.name ?? mPlan?.name ?? resource.plan?.name ?? ""

  return (
    <div className="flex animate-[pt-slide_.3s_both] flex-col gap-3.5">
      <BackButton />
      <DetailHeading
        eyebrow={`${where(context, resource.namespace)} · ${kind === "schema" ? "Schema plan" : "Migration plan"} for ${resource.name}`}
        title={planName}
        sub={waiting ? planSummary(resource) : "This plan is not waiting for approval now."}
      />
      <Card title="What this changes">
        <RiskList risks={planRisks(resource, sPlan)} />
      </Card>
      {planError ? <ReadError error={planError} what="the plan" /> : null}
      {sPlan ? <SchemaSql plan={sPlan} /> : null}
      {mPlan ? <MigrationSteps plan={mPlan} /> : null}
      {sPlan || mPlan ? (
        <Card title="Bound to">
          <BindingTable rows={bindings(resource, sPlan, mPlan)} />
          <span className="font-mono text-xs break-all text-mute">
            fingerprint {(sPlan ?? mPlan)?.fingerprint}
          </span>
        </Card>
      ) : schemaPlan.isPending || migrationPlan.isPending ? (
        <Loading what="the plan's SQL" />
      ) : null}
      <div className="sticky bottom-3 flex flex-wrap items-center gap-2 rounded-[18px] border border-line bg-card p-3 shadow-card">
        {waiting ? (
          mayApprove === "allowed" ? (
            <Button
              variant="primary"
              className="flex-[1_1_200px]"
              disabled={!(sPlan ?? mPlan)}
              onClick={() => setApproving(true)}
            >
              Approve this plan…
            </Button>
          ) : (
            <span className="flex-auto p-2.5 text-sm text-fg2">
              {mayApprove === "read-only"
                ? readOnlyText
                : `This identity may not approve plans in ${resource.namespace}. Share the link with an approver.`}
            </span>
          )
        ) : (
          <span className="flex-auto p-2.5 text-sm font-semibold text-green">
            ✓ Not waiting for approval.{" "}
            <Link
              to="/releases/$namespace/$kind/$name"
              params={{ namespace: resource.namespace, kind: resource.kind, name: resource.name }}
            >
              See where it stands
            </Link>
          </span>
        )}
        <Link
          to="/approve"
          className="inline-flex min-h-11 items-center justify-center rounded-xl border border-line bg-card px-4 text-sm font-medium text-fg"
        >
          Not now
        </Link>
      </div>
      {approving ? (
        <ApproveDialog resource={resource} context={context} onClose={() => setApproving(false)} />
      ) : null}
    </div>
  )
}

function SchemaSql({ plan }: { plan: SchemaPlan }) {
  if (plan.sqlAccess !== SqlAccess.AVAILABLE) {
    return (
      <Card title="Exact SQL">
        <span className="text-sm text-fg2">{plan.sqlProblem || "The SQL is not shown."}</span>
      </Card>
    )
  }
  return (
    <Card title="Exact SQL · plain text">
      <SqlView
        lines={plan.statements.map((statement, index) => {
          const tag = statementTag(statement)
          return { number: index + 1, sql: statement.sql, tag: tag.tag, tagTone: tag.tone }
        })}
      />
    </Card>
  )
}

function MigrationSteps({ plan }: { plan: MigrationPlan }) {
  return (
    <Card title="Steps in order">
      <ol className="m-0 list-none overflow-hidden rounded-xl border border-line2 p-0">
        {plan.migrations.map((step) => (
          <li
            key={step.versionKey || String(step.version)}
            className="grid grid-cols-[64px_minmax(0,1fr)_96px] gap-2 border-b border-line2 px-2.5 py-2 font-mono text-[12.5px] last:border-b-0"
          >
            <span>{step.versionKey || String(step.version)}</span>
            <span className="truncate">{step.description}</span>
            <span className="text-mute">{step.checksum.slice(0, 9)}</span>
          </li>
        ))}
      </ol>
      <span className="text-[13px] leading-normal text-fg2">
        This operator API publishes files and checksums, not their SQL. Read the files in the
        artifact&apos;s repository.
      </span>
    </Card>
  )
}

function bindings(
  resource: ResourceView,
  schema?: SchemaPlan,
  migration?: MigrationPlan
): Binding[] {
  const rows: Binding[] = []
  const digest = schema?.artifactDigest ?? migration?.artifactDigest
  if (digest) {
    rows.push({ name: "Artifact", value: shortDigest(digest, 8) })
  }
  const created = schema?.createdAt ?? migration?.createdAt
  if (created) {
    rows.push({ name: "Computed at", value: timestampDate(created).toLocaleString() })
  }
  if (migration) {
    rows.push({ name: "History read at", value: `version ${String(migration.currentVersion)}` })
  }
  rows.push({ name: "Policy", value: `apply: ${resource.applyPolicy || "–"}` })
  const contract = schema?.contractVersion ?? migration?.contractVersion
  if (contract) {
    rows.push({ name: "Plan contract", value: String(contract) })
  }
  if (schema?.ptahVersion) {
    rows.push({ name: "Planned by", value: `Ptah ${schema.ptahVersion}` })
  }
  return rows
}
