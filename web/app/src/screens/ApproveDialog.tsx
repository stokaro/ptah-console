import { Code, ConnectError, createClient } from "@connectrpc/connect"
import { skipToken, useMutation, useQuery, useTransport } from "@connectrpc/connect-query"
import { shortDigest } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useState } from "react"

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
  /** The plan the person read, when they read it on the plan page. */
  reviewed?: ReviewedPlan
}

/** The plan a person reviewed, as it was when they chose to approve it. */
export interface ReviewedPlan {
  ownerUid: string
  name: string
  uid: string
  fingerprint: string
  contractVersion: number
}

/**
 * Approves exactly the plan the person reviewed, as the session's identity.
 * The plan is held from the moment the dialog opens: if the resource moves on
 * while it is open, the request still names what was reviewed, and the server,
 * which reads the resource again, answers that it changed.
 */
export function ApproveDialog({ resource, context, onClose, reviewed }: ApproveDialogProps) {
  const transport = useTransport()
  const queryClient = useQueryClient()
  const session = useSession()
  const isSchema = resource.kind === "schema"
  const [held, setHeld] = useState<ReviewedPlan | undefined>(reviewed)
  const schemaPlan = useQuery(
    SchemaService.method.getSchemaPlan,
    isSchema && !held
      ? { namespace: resource.namespace, schema: resource.name, selection: PlanSelection.CURRENT }
      : skipToken
  )
  const migrationPlan = useQuery(
    MigrationService.method.getMigrationPlan,
    !isSchema && !held ? { namespace: resource.namespace, migration: resource.name } : skipToken
  )
  const approveSchema = useMutation(DecisionService.method.approveSchemaPlan)
  const approveMigration = useMutation(DecisionService.method.approveMigrationPlan)

  const read = isSchema ? schemaPlan.data?.plan : migrationPlan.data?.plan
  if (!held && read) {
    // The first reading is the plan this dialog approves, whatever later
    // readings say.
    setHeld({
      ownerUid: resource.ref.uid,
      name: read.name,
      uid: read.uid,
      fingerprint: read.fingerprint,
      contractVersion: read.contractVersion,
    })
  }
  const plan = held
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
          schemaUid: plan?.ownerUid ?? "",
          contractVersion: plan?.contractVersion ?? 0,
        })
      : await approveMigration.mutateAsync({
          ...common,
          migrationName: resource.name,
          migrationUid: plan?.ownerUid ?? "",
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
