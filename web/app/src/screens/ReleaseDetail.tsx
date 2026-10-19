import { create } from "@bufbuild/protobuf"
import { useMutation } from "@connectrpc/connect-query"
import { Button, Card, EventList, StepRail } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { getRouteApi } from "@tanstack/react-router"
import { useState } from "react"

import { can, useResources, useSession } from "../api/resources"
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
  const { resources, scope, loading, error } = useResources()
  const session = useSession()
  const now = useNow(5_000)
  const [suspending, setSuspending] = useState(false)
  const [copied, setCopied] = useState<boolean>()
  const resource = resources.find(
    (r) => r.kind === kind && r.namespace === params.namespace && r.name === params.name
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
  const maySuspend = can(
    scope.scopes,
    resource.namespace,
    kind === "schema" ? "suspendSchemas" : "suspendMigrations"
  )
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
          events={history(resource).map((condition) => ({
            key: condition.type,
            time: clock(condition.lastTransition),
            tone: conditionTone(condition),
            title: `${condition.type} · ${condition.reason}`,
            detail: condition.message,
          }))}
        />
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
          <span role="status" className="text-[12.5px] text-mute">
            {copied ? "Link copied." : "The browser did not allow copying."}
          </span>
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
      explanation={
        suspend
          ? `The operator stops planning and applying it until it is resumed. Only spec.suspend changes.${gitops}`
          : `The operator plans and applies it again. Only spec.suspend changes.${gitops}`
      }
      ready
      send={send}
      admittedTitle={suspend ? "Suspended" : "Resumed"}
      admittedText={() =>
        `${resource.name} is ${suspend ? "suspended" : "resumed"}. The operator reads the change on its next pass.`
      }
    />
  )
}
