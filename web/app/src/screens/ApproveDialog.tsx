import { Code, ConnectError, createClient } from "@connectrpc/connect"
import { skipToken, useMutation, useQuery, useTransport } from "@connectrpc/connect-query"
import { shortDigest } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { useSession } from "../api/resources"
import { DecisionKind, DecisionService } from "../gen/ptah/console/v1/decisions_pb"
import { MigrationService, PlanSelection, SchemaService } from "../gen/ptah/console/v1/resources_pb"
import type { ResourceView } from "../model/resource"
import { where } from "./common"
import { DecisionDialog } from "./DecisionDialog"

export interface ApproveDialogProps {
  resource: ResourceView
  context?: string
  onClose: () => void
}

/** Approves exactly the plan the resource names now, as the session's identity. */
export function ApproveDialog({ resource, context, onClose }: ApproveDialogProps) {
  const transport = useTransport()
  const queryClient = useQueryClient()
  const session = useSession()
  const isSchema = resource.kind === "schema"
  const schemaPlan = useQuery(
    SchemaService.method.getSchemaPlan,
    isSchema
      ? { namespace: resource.namespace, schema: resource.name, selection: PlanSelection.CURRENT }
      : skipToken
  )
  const migrationPlan = useQuery(
    MigrationService.method.getMigrationPlan,
    isSchema ? skipToken : { namespace: resource.namespace, migration: resource.name }
  )
  const approveSchema = useMutation(DecisionService.method.approveSchemaPlan)
  const approveMigration = useMutation(DecisionService.method.approveMigrationPlan)

  const plan = isSchema ? schemaPlan.data?.plan : migrationPlan.data?.plan
  const fingerprint = plan?.fingerprint ?? ""
  const kind = isSchema ? DecisionKind.SCHEMA_APPROVAL : DecisionKind.MIGRATION_APPROVAL

  async function send() {
    const common = {
      namespace: resource.namespace,
      planName: plan?.name ?? "",
      planUid: plan?.uid ?? "",
      planFingerprint: fingerprint,
    }
    const response = isSchema
      ? await approveSchema.mutateAsync({
          ...common,
          schemaName: resource.name,
          schemaUid: resource.ref.uid,
          contractVersion: schemaPlan.data?.plan?.contractVersion ?? 0,
        })
      : await approveMigration.mutateAsync({
          ...common,
          migrationName: resource.name,
          migrationUid: resource.ref.uid,
        })
    void queryClient.invalidateQueries()
    return response.result!
  }

  async function readBack(name: string) {
    try {
      const response = await createClient(DecisionService, transport).getDecision({
        kind,
        namespace: resource.namespace,
        name,
      })
      return response.decision
    } catch (error) {
      if (ConnectError.from(error).code === Code.NotFound) {
        return undefined
      }
      throw error
    }
  }

  const followLink = {
    to: "/releases/$namespace/$kind/$name",
    params: { namespace: resource.namespace, kind: resource.kind, name: resource.name },
  } as const

  return (
    <DecisionDialog
      onClose={onClose}
      where={where(context, resource.namespace)}
      as={session.data?.kubernetesUsername ?? "…"}
      title={`Approve ${plan?.name ?? resource.plan?.name ?? "this plan"}?`}
      explanation={
        plan
          ? `Sends the exact plan you reviewed (${shortDigest(fingerprint)}). Admission records you as the approver.`
          : "Reading the plan…"
      }
      needsReview
      ready={Boolean(plan)}
      send={send}
      readBack={readBack}
      admittedTitle="Approval admitted"
      admittedText={(decision) =>
        `${plan?.name ?? ""} · recorded by admission as ${decision?.decidedBy ?? "you"}. Approved is not yet applied: the operator runs it next.`
      }
      next={
        <Link
          {...followLink}
          onClick={onClose}
          className="inline-flex min-h-11 items-center justify-center rounded-xl bg-linear-135 from-pri to-[color-mix(in_oklab,var(--pri)_62%,var(--teal))] px-[18px] text-sm font-semibold text-onpri"
        >
          Follow it
        </Link>
      }
      reviewAgain={
        <Link
          to="/approve/$namespace/$kind/$name"
          params={followLink.params}
          onClick={onClose}
          className="inline-flex min-h-11 items-center justify-center rounded-xl bg-linear-135 from-pri to-[color-mix(in_oklab,var(--pri)_62%,var(--teal))] px-[18px] text-sm font-semibold text-onpri"
        >
          Review the new plan
        </Link>
      }
    />
  )
}
