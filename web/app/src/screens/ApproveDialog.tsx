import { Code, ConnectError, createClient } from "@connectrpc/connect"
import { skipToken, useMutation, useQuery, useTransport } from "@connectrpc/connect-query"
import { shortDigest } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useState } from "react"

import { useSession } from "../api/resources"
import { DecisionKind, DecisionService } from "../gen/ptah/console/v1/decisions_pb"
import { MigrationService, PlanSelection, SchemaService } from "../gen/ptah/console/v1/resources_pb"
import { readingAgain, type ResourceView } from "../model/resource"
import { where } from "./common"
import { DecisionDialog } from "./DecisionDialog"

export interface ApproveDialogProps {
  resource: ResourceView
  context?: string
  onClose: () => void
  /** The plan the person read, when they read it on the plan page. */
  reviewed?: ReviewedPlan
  /**
   * The pages the dialog leads on to: Focus's, or, opened in the full
   * console, the console's own, so a person stays where they started.
   */
  pages?: "focus" | "console"
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
export function ApproveDialog({
  resource,
  context,
  onClose,
  reviewed,
  pages = "focus",
}: ApproveDialogProps) {
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

  return (
    <DecisionDialog
      onClose={onClose}
      where={where(context, resource.namespace)}
      as={session.data?.kubernetesUsername ?? "…"}
      title={`Approve ${plan?.name ?? resource.plan?.name ?? "this plan"}?`}
      explanation={
        !plan
          ? "Reading the plan…"
          : resource.approvalOpen
            ? `Sends the exact plan you reviewed (${shortDigest(fingerprint)}). Admission records you as the approver.`
            : `${readingAgain(resource)} Confirm waits for it.`
      }
      needsReview
      ready={Boolean(plan) && resource.approvalOpen}
      send={send}
      readBack={readBack}
      admittedTitle="Approval admitted"
      admittedText={(decision) =>
        `${plan?.name ?? ""} · recorded by admission as ${decision?.decidedBy ?? "you"}. Approved is not yet applied: the operator runs it next.`
      }
      next={<FollowLink resource={resource} pages={pages} onClick={onClose} />}
      reviewAgain={<ReviewAgainLink resource={resource} pages={pages} onClick={onClose} />}
    />
  )
}

const nextClass =
  "inline-flex min-h-11 items-center justify-center rounded-xl bg-linear-135 from-pri to-[color-mix(in_oklab,var(--pri)_62%,var(--teal))] px-[18px] text-sm font-semibold text-onpri"

interface NextLinkProps {
  resource: ResourceView
  pages: "focus" | "console"
  onClick: () => void
}

/** The resource's own page, where it is followed while the operator applies the plan. */
function FollowLink({ resource, pages, onClick }: NextLinkProps) {
  const params = { namespace: resource.namespace, name: resource.name }
  if (pages === "focus") {
    return (
      <Link
        to="/releases/$namespace/$kind/$name"
        params={{ ...params, kind: resource.kind }}
        onClick={onClick}
        className={nextClass}
      >
        Follow it
      </Link>
    )
  }
  return (
    <Link
      to={
        resource.kind === "schema"
          ? "/console/schemas/$namespace/$name"
          : "/console/migrations/$namespace/$name"
      }
      params={params}
      onClick={onClick}
      className={nextClass}
    >
      Follow it
    </Link>
  )
}

/** The plan that replaced the one the person reviewed. */
function ReviewAgainLink({ resource, pages, onClick }: NextLinkProps) {
  const params = { namespace: resource.namespace, name: resource.name }
  if (pages === "focus") {
    return (
      <Link
        to="/approve/$namespace/$kind/$name"
        params={{ ...params, kind: resource.kind }}
        onClick={onClick}
        className={nextClass}
      >
        Review the new plan
      </Link>
    )
  }
  return (
    <Link
      to={
        resource.kind === "schema"
          ? "/console/schemas/$namespace/$name/plan"
          : "/console/migrations/$namespace/$name/plan"
      }
      params={params}
      search={{}}
      onClick={onClick}
      className={nextClass}
    >
      Review the new plan
    </Link>
  )
}
