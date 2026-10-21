import { skipToken, useQuery } from "@connectrpc/connect-query"
import { BindingTable, Card, Terminal } from "@ptah/console-ui"
import { getRouteApi } from "@tanstack/react-router"

import { useResources, useSession } from "../api/resources"
import { DecisionKind, DecisionService } from "../gen/ptah/console/v1/decisions_pb"
import { PlanSelection, SchemaService, SqlAccess } from "../gen/ptah/console/v1/resources_pb"
import { decisionKind, decisionState, decisionWhat, retentionText } from "../model/decision"
import { BackButton, DetailHeading, Loading, ReadError, where } from "./common"
import { decidedAt } from "./IncidentDetail"

const route = getRouteApi("/_focus/decisions/$namespace/$kind/$name")

export function DecisionDetail() {
  const params = route.useParams()
  const kind = decisionKind(params.kind)
  const session = useSession()
  const { resources } = useResources()
  const query = useQuery(
    DecisionService.method.getDecision,
    kind ? { kind, namespace: params.namespace, name: params.name } : skipToken
  )
  const decision = query.data?.decision
  const owner = resources.find(
    (r) => r.kind === "schema" && r.namespace === params.namespace && r.name === decision?.ownerName
  )
  // The approved plan's SQL is shown while the schema still holds that plan,
  // as its current plan or as the one last applied.
  const selection =
    decision?.kind === DecisionKind.SCHEMA_APPROVAL && owner
      ? owner.plan?.uid === decision.planUid
        ? PlanSelection.CURRENT
        : owner.appliedPlanUid === decision.planUid
          ? PlanSelection.APPLIED
          : undefined
      : undefined
  const plan = useQuery(
    SchemaService.method.getSchemaPlan,
    selection ? { namespace: params.namespace, schema: owner!.name, selection } : skipToken
  )

  if (query.isPending && kind) {
    return <Loading what="the decision" />
  }
  if (query.error || !decision) {
    return (
      <>
        <BackButton />
        {query.error ? (
          <ReadError error={query.error} what="the decision" />
        ) : (
          <Card title="Not found">
            <span className="text-sm">No such decision.</span>
          </Card>
        )}
      </>
    )
  }
  const state = decisionState(decision.state)
  const sql =
    plan.data?.plan?.sqlAccess === SqlAccess.AVAILABLE
      ? plan.data.plan.statements.map((s) => s.sql).join("\n\n")
      : undefined

  return (
    <div className="flex animate-[pt-slide_.3s_both] flex-col gap-3.5">
      <BackButton />
      <DetailHeading
        eyebrow={`${decidedAt(decision.decidedAt)} · ${where(session.data?.context, params.namespace)}`}
        title={decisionWhat(decision)}
        mono={false}
        sub={`by ${decision.decidedBy}, recorded by admission · ${state.label}${decision.stateReason ? `: ${decision.stateReason}` : ""}`}
      />
      <Card title="Identity of the decision">
        <BindingTable
          rows={[
            { name: "Object", value: `${decision.ref?.name ?? ""} (${decision.ref?.uid ?? ""})` },
            { name: "Owner", value: `${decision.ownerName} (${decision.ownerUid})` },
            ...(decision.planName
              ? [{ name: "Plan", value: `${decision.planName} (${decision.planUid})` }]
              : []),
            ...(decision.planFingerprint
              ? [{ name: "Fingerprint", value: decision.planFingerprint }]
              : []),
            ...(decision.operationId ? [{ name: "Operation", value: decision.operationId }] : []),
            { name: "Groups", value: decision.decidedByGroups.join(", ") || "–" },
          ]}
        />
      </Card>
      {decision.kind === DecisionKind.SCHEMA_APPROVAL ? (
        <Card title="Exact SQL that was approved · plain text">
          {sql ? (
            <Terminal label="Approved SQL" text={sql} />
          ) : (
            <span className="text-sm text-fg2">
              {retentionText(decision) ||
                (plan.isFetching
                  ? "Reading the plan…"
                  : selection
                    ? plan.data?.plan?.sqlProblem || "The SQL is not shown."
                    : "The schema no longer names this plan as current or applied, so its SQL is not shown here.")}
            </span>
          )}
        </Card>
      ) : null}
    </div>
  )
}
