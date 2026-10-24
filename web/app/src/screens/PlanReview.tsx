import { skipToken, useQuery } from "@connectrpc/connect-query"
import { timestampDate } from "@bufbuild/protobuf/wkt"
import {
  BindingTable,
  Fingerprint,
  SequenceTable,
  Button,
  Card,
  FindingList,
  SqlView,
  shortDigest,
  type Binding,
} from "@ptah/console-ui"
import { getRouteApi, Link } from "@tanstack/react-router"
import { useState } from "react"

import { offerText, useOffer, useResource, useSession } from "../api/resources"
import {
  MigrationService,
  PlanSelection,
  SchemaService,
  SourceKind,
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
import { ApproveDialog, type ReviewedPlan } from "./ApproveDialog"
import { BackButton, DetailHeading, Loading, ReadError, where } from "./common"

const route = getRouteApi("/_focus/approve/$namespace/$kind/$name")

export function PlanReview() {
  const params = route.useParams()
  const kind = parseKind(params.kind)
  const { resource, loading, error } = useResource(kind, params.namespace, params.name)
  const session = useSession()
  const offer = useOffer()
  const [approving, setApproving] = useState<ReviewedPlan>()
  // The plan this person started reading. When the operator replaces it, the
  // page shows the new one, says so, and offers no approval until they
  // confirm they are reading the new one.
  const [readUid, setReadUid] = useState<string>()
  // While the operator plans again there is no current plan to read.
  const hasPlan = Boolean(resource?.plan)
  const schemaPlan = useQuery(
    SchemaService.method.getSchemaPlan,
    kind === "schema" && hasPlan
      ? { namespace: params.namespace, schema: params.name, selection: PlanSelection.CURRENT }
      : skipToken
  )
  const migrationPlan = useQuery(
    MigrationService.method.getMigrationPlan,
    kind === "migration" && hasPlan
      ? { namespace: params.namespace, migration: params.name }
      : skipToken
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
      <Card title="What this changes">
        <FindingList findings={planRisks(resource, sPlan)} />
      </Card>
      {planError ? <ReadError error={planError} what="the plan" /> : null}
      {sPlan ? <SchemaSql plan={sPlan} /> : null}
      {mPlan ? <MigrationSteps plan={mPlan} /> : null}
      {sPlan || mPlan ? (
        <Card title="Bound to">
          <BindingTable rows={bindings(resource, sPlan, mPlan)} />
          <Fingerprint value={(sPlan ?? mPlan)?.fingerprint ?? ""} />
        </Card>
      ) : schemaPlan.isLoading || migrationPlan.isLoading ? (
        <Loading what="the plan's SQL" />
      ) : !hasPlan ? (
        <Card title="The plan">
          <span className="text-sm leading-normal">
            The operator is planning again, as it does each interval. The plan appears here once it
            is published.
          </span>
        </Card>
      ) : null}
      <div className="sticky bottom-3 flex flex-wrap items-center gap-2 rounded-[18px] border border-line bg-card p-3 shadow-card">
        {waiting && !hasPlan ? (
          <span className="flex-auto p-2.5 text-sm text-fg2">
            Nothing to approve until the operator publishes the plan again. This page follows it.
          </span>
        ) : waiting ? (
          mayApprove === "allowed" ? (
            <Button
              variant="primary"
              className="flex-[1_1_200px]"
              disabled={!shown || replaced}
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
          ) : (
            <span className="flex-auto p-2.5 text-sm text-fg2">
              {offerText(
                mayApprove,
                `This identity may not approve plans in ${resource.namespace}. Share the link with an approver.`
              )}
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
        <ApproveDialog
          resource={resource}
          context={context}
          reviewed={approving}
          onClose={() => setApproving(undefined)}
        />
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

/** A migration plan's files in order, which is all this operator API publishes of it. */
export function MigrationSteps({ plan }: { plan: MigrationPlan }) {
  return (
    <Card title="Steps in order">
      <SequenceTable
        caption="Steps in order"
        rows={plan.migrations.map((step) => ({
          version: step.versionKey || String(step.version),
          name: step.description,
          note: step.checksum.slice(0, 9),
        }))}
      />
      <span className="text-[13px] leading-normal text-fg2">
        This operator API publishes files and checksums, not their SQL. Read the files in the
        artifact&apos;s repository.
      </span>
    </Card>
  )
}

/** What a plan is bound to, as an approval of it is. */
export function bindings(
  resource: ResourceView,
  schema?: SchemaPlan,
  migration?: MigrationPlan
): Binding[] {
  const rows: Binding[] = []
  const digest = schema?.artifactDigest ?? migration?.artifactDigest
  if (digest) {
    rows.push({ name: "Artifact", value: shortDigest(digest, 8) })
  }
  const source = schema?.source ?? migration?.source
  if (source) {
    rows.push({
      name: "Source",
      value: `${source.kind === SourceKind.GIT ? "Git" : "ConfigMap"}, content ${shortDigest(source.contentDigest, 8)}`,
    })
    if (source.policyDigest) {
      rows.push({ name: "Source policy", value: shortDigest(source.policyDigest, 8) })
    }
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
