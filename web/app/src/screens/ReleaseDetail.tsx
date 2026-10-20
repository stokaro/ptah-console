import { create } from "@bufbuild/protobuf"
import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query"
import { Button, Card, EventList, StepRail, Toast, type TimelineEvent } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { getRouteApi } from "@tanstack/react-router"
import { useState } from "react"

import { can, useOffer, useResource, useSession } from "../api/resources"
import {
  DiagnosticsService,
  Evidence,
  type EventList as EventListMessage,
} from "../gen/ptah/console/v1/diagnostics_pb"
import { StageState } from "../gen/ptah/console/v1/resources_pb"
import {
  DecisionResultSchema,
  DecisionService,
  ResourceKind,
} from "../gen/ptah/console/v1/decisions_pb"
import {
  clock,
  conditionTone,
  history,
  parseKind,
  say,
  steps,
  waitingForApproval,
  type ResourceView,
} from "../model/resource"
import { BackButton, copyText, DetailHeading, Loading, ReadError, useNow, where } from "./common"
import { DecisionDialog } from "./DecisionDialog"
import { Sentence } from "./Releases"

const route = getRouteApi("/releases/$namespace/$kind/$name")

export function ReleaseDetail() {
  const params = route.useParams()
  const kind = parseKind(params.kind)
  const { resource, scope, loading, error } = useResource(kind, params.namespace, params.name)
  const session = useSession()
  const offer = useOffer()
  const now = useNow(5_000)
  const [suspending, setSuspending] = useState(false)
  const [copied, setCopied] = useState<boolean>()
  // Events about the resource itself, when the identity may list them.
  const events = useQuery(
    DiagnosticsService.method.getDiagnostics,
    resource && can(scope.scopes, resource.namespace, "readEvents")
      ? {
          kind: resource.kind === "schema" ? ResourceKind.SCHEMA : ResourceKind.MIGRATION,
          namespace: resource.namespace,
          name: resource.name,
          uid: resource.ref.uid,
        }
      : skipToken
  )

  if (loading) {
    return <Loading what="the release" />
  }
  if (error) {
    return <ReadError error={error} what="the release" />
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
  const sentence = say(resource, now)
  const waiting = waitingForApproval(resource)
  const next = resource.attention[0]?.nextStep
  const maySuspend =
    offer(resource.namespace, kind === "schema" ? "suspendSchemas" : "suspendMigrations") ===
    "allowed"
  const approvalPath = `/approve/${encodeURIComponent(resource.namespace)}/${resource.kind}/${encodeURIComponent(resource.name)}`

  return (
    <div className="flex animate-[pt-slide_.3s_both] flex-col gap-3.5">
      <BackButton />
      <DetailHeading
        eyebrow={`${where(context, resource.namespace)} · ${kind === "schema" ? "Schema" : "Migration"}`}
        title={resource.name}
        sub={<span className="font-mono text-xs break-all">{resource.source}</span>}
      />
      <Card label="Where it stands">
        <StepRail steps={steps(resource)} label={`Progress of ${resource.name}`} />
        <Sentence tone={sentence.tone} text={sentence.text} large />
        {!resource.statusCurrent ? (
          <span className="text-[12.5px] text-mute">
            The operator has not yet written a status for the latest change to this resource.
          </span>
        ) : null}
      </Card>
      <Card title="What happened">
        <EventList
          label={`What happened to ${resource.name}`}
          events={happened(resource, events.data?.events)}
        />
        {events.data?.events && events.data.events.evidence !== Evidence.SHOWN ? (
          <span className="text-xs text-mute">
            Kubernetes Events are not shown:{" "}
            {events.data.events.problem || "this identity may not list them."}
          </span>
        ) : null}
      </Card>
      <Card title="What you can do">
        <span className="text-sm leading-normal">
          {next ||
            (sentence.tone === "ok"
              ? "Nothing to do."
              : "Wait for the operator; this page follows it.")}
        </span>
        <div className="flex flex-wrap gap-2">
          {waiting ? (
            <Button
              variant="primary"
              onClick={() =>
                void copyText(new URL(approvalPath, window.location.origin).toString()).then(
                  setCopied
                )
              }
            >
              Copy link for approvers
            </Button>
          ) : null}
          {maySuspend ? (
            <Button onClick={() => setSuspending(true)}>
              {resource.suspended ? "Resume…" : "Suspend…"}
            </Button>
          ) : null}
        </div>
        {copied !== undefined ? (
          <Toast
            title={copied ? "Link copied" : "The browser did not allow copying"}
            detail={copied ? new URL(approvalPath, window.location.origin).toString() : undefined}
          />
        ) : null}
      </Card>
      {suspending ? (
        <SuspendDialog resource={resource} context={context} onClose={() => setSuspending(false)} />
      ) : null}
    </div>
  )
}

interface SuspendDialogProps {
  resource: ResourceView
  context?: string
  onClose: () => void
}

/** Suspends or resumes the resource at the version the page shows. */
function SuspendDialog({ resource, context, onClose }: SuspendDialogProps) {
  const session = useSession()
  const queryClient = useQueryClient()
  const setSuspended = useMutation(DecisionService.method.setSuspended)
  const suspend = !resource.suspended
  const gitops = resource.gitops?.controller
    ? ` ${resource.gitops.controller} manages this object; its next sync may set it back.`
    : ""

  async function send() {
    const response = await setSuspended.mutateAsync({
      kind: resource.kind === "schema" ? ResourceKind.SCHEMA : ResourceKind.MIGRATION,
      namespace: resource.namespace,
      name: resource.name,
      uid: resource.ref.uid,
      resourceVersion: resource.ref.resourceVersion,
      suspended: suspend,
    })
    void queryClient.invalidateQueries()
    return create(DecisionResultSchema, { outcome: response.outcome, message: response.message })
  }

  return (
    <DecisionDialog
      onClose={onClose}
      where={where(context, resource.namespace)}
      as={session.data?.kubernetesUsername ?? "…"}
      title={`${suspend ? "Suspend" : "Resume"} ${resource.name}?`}
      explanation={suspendText(resource, suspend) + gitops}
      ready
      send={send}
      admittedTitle={suspend ? "Suspended" : "Resumed"}
      admittedText={() =>
        `${resource.name} is ${suspend ? "suspended" : "resumed"}. The operator reads the change on its next pass.`
      }
    />
  )
}

/**
 * What suspending or resuming does, as the operator documents it. Only
 * spec.suspend changes, but any spec change is an input to the plan.
 */
export function suspendText(resource: ResourceView, suspend: boolean) {
  if (!suspend) {
    return "The operator reads the database again and plans before it applies anything. Only spec.suspend changes."
  }
  const applying = resource.stages.some(
    (stage) => stage.name === "Apply" && stage.state === StageState.ACTIVE
  )
  const text =
    "The operator starts no new Job until it is resumed. Only spec.suspend changes, but a spec change retires the current plan and any approval of it."
  if (!applying) {
    return text
  }
  return resource.kind === "migration"
    ? `${text} An Apply is running: suspending now leaves a run whose result is not read, and a person has to acknowledge it. Let it finish first.`
    : `${text} An Apply is running; the operator watches it until it ends and does not stop it.`
}

/**
 * What happened, oldest first: the resource's conditions as the operator
 * last wrote them, and the Kubernetes Events about it that are still kept.
 */
export function happened(resource: ResourceView, events?: EventListMessage): TimelineEvent[] {
  const rows: { at: bigint; row: TimelineEvent }[] = history(resource).map((condition) => ({
    at: condition.lastTransition?.seconds ?? 0n,
    row: {
      key: `condition-${condition.type}`,
      time: clock(condition.lastTransition),
      tone: conditionTone(condition),
      title: `${condition.type} · ${condition.reason}`,
      detail: condition.message,
    },
  }))
  if (events?.evidence === Evidence.SHOWN) {
    events.events.forEach((event, index) => {
      rows.push({
        at: event.lastSeen?.seconds ?? 0n,
        row: {
          key: `event-${index}`,
          time: clock(event.lastSeen),
          tone: event.type === "Warning" ? "warn" : "ok",
          title: `Event · ${event.reason}${event.count > 1 ? ` ×${event.count}` : ""}`,
          detail: event.message,
        },
      })
    })
  }
  return rows.sort((a, b) => (a.at < b.at ? -1 : a.at > b.at ? 1 : 0)).map(({ row }) => row)
}
