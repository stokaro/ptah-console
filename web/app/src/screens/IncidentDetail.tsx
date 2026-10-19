import { timestampDate } from "@bufbuild/protobuf/wkt"
import { Code, ConnectError, createClient } from "@connectrpc/connect"
import { skipToken, useMutation, useQuery, useTransport } from "@connectrpc/connect-query"
import { Button, Card, EventList, Terminal, type TimelineEvent } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { getRouteApi } from "@tanstack/react-router"
import { useState } from "react"

import { can, readOnlyText, useOffer, useResources, useSession } from "../api/resources"
import { DecisionKind, DecisionService, ResourceKind } from "../gen/ptah/console/v1/decisions_pb"
import {
  DiagnosticsService,
  Evidence,
  type EventList as EventListMessage,
  type GetDiagnosticsResponse,
} from "../gen/ptah/console/v1/diagnostics_pb"
import { AttentionKind } from "../gen/ptah/console/v1/resources_pb"
import {
  attentionTitle,
  attentionTone,
  clock,
  incident,
  parseKind,
  type ResourceView,
} from "../model/resource"
import { BackButton, DetailHeading, Loading, ReadError, where } from "./common"
import { DecisionDialog } from "./DecisionDialog"

const route = getRouteApi("/incidents/$namespace/$kind/$name")

const titleColors = {
  ok: "text-green",
  warn: "text-amber",
  bad: "text-red",
  unk: "text-violet",
  run: "text-pri",
  off: "text-mute",
} as const

export function IncidentDetail() {
  const params = route.useParams()
  const kind = parseKind(params.kind)
  const { resources, scope, loading, error } = useResources()
  const session = useSession()
  const offer = useOffer()
  const [acknowledging, setAcknowledging] = useState(false)
  const resource = resources.find(
    (r) => r.kind === kind && r.namespace === params.namespace && r.name === params.name
  )
  const readLogs = resource ? can(scope.scopes, resource.namespace, "readLogs") : false
  const diagnostics = useQuery(
    DiagnosticsService.method.getDiagnostics,
    resource
      ? {
          kind: resource.kind === "schema" ? ResourceKind.SCHEMA : ResourceKind.MIGRATION,
          namespace: resource.namespace,
          name: resource.name,
          uid: resource.ref.uid,
          includeLogs: readLogs,
        }
      : skipToken
  )

  if (loading) {
    return <Loading what="the resource" />
  }
  if (error) {
    return <ReadError error={error} what="the resource" />
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
  const attention = incident(resource) ?? resource.attention[0]
  const tone = attention ? attentionTone(attention.kind) : "ok"
  const unresolved = attention?.kind === AttentionKind.UNRESOLVED_RUN && resource.unresolvedRun
  const mayAcknowledge = offer(resource.namespace, "acknowledgeRuns")
  const log = diagnostics.data ? lastLog(diagnostics.data) : undefined

  return (
    <div className="flex animate-[pt-slide_.3s_both] flex-col gap-3.5">
      <BackButton />
      <DetailHeading
        eyebrow={`${where(session.data?.context, resource.namespace)} · ${resource.name}`}
        title={attention ? attentionTitle(attention.kind) : "Nothing needs a person"}
        mono={false}
        sub={<span className={`text-[14.5px] ${titleColors[tone]}`}>{attention?.reason}</span>}
      />
      <Card title="Next supported step">
        <span className="text-[15px] leading-normal">
          {attention?.nextStep || "Nothing to do."}
        </span>
        {unresolved ? (
          mayAcknowledge === "allowed" ? (
            <Button
              variant="attention"
              className="self-start"
              onClick={() => setAcknowledging(true)}
            >
              Acknowledge this run…
            </Button>
          ) : (
            <span className="text-[13px] text-fg2">
              {mayAcknowledge === "read-only"
                ? readOnlyText
                : `This identity may not acknowledge runs in ${resource.namespace}.`}
            </span>
          )
        ) : null}
      </Card>
      <Card title="Evidence still kept">
        {diagnostics.isPending ? (
          <Loading what="the evidence" />
        ) : diagnostics.error ? (
          <ReadError error={diagnostics.error} what="the evidence" />
        ) : (
          <EventList label="Evidence" events={evidence(diagnostics.data!)} />
        )}
        <span className="text-xs text-mute">
          &ldquo;Not retained&rdquo; means Kubernetes no longer keeps it, not that it never
          happened.
        </span>
      </Card>
      {log ? (
        <Card title={`Log excerpt · ${log.where} · plain text`}>
          <Terminal label="Log excerpt" text={log.text} />
          {log.truncated ? (
            <span className="text-xs text-mute">Earlier lines are not shown.</span>
          ) : null}
        </Card>
      ) : null}
      {acknowledging && unresolved ? (
        <AcknowledgeDialog
          resource={resource}
          context={session.data?.context}
          onClose={() => setAcknowledging(false)}
        />
      ) : null}
    </div>
  )
}

function eventsOf(list: EventListMessage | undefined, about: string, key: string): TimelineEvent[] {
  if (!list) {
    return []
  }
  if (list.evidence !== Evidence.SHOWN) {
    return [
      {
        key: `${key}-events`,
        time: "–",
        tone: "off",
        title: `Events about ${about}: ${evidenceWord(list.evidence)}`,
        detail: list.problem,
      },
    ]
  }
  return list.events.map((event, index) => ({
    key: `${key}-${index}`,
    time: clock(event.lastSeen),
    tone: event.type === "Warning" ? "warn" : "ok",
    title: `${event.reason}${event.count > 1 ? ` ×${event.count}` : ""}`,
    detail: `${about}: ${event.message}`,
  }))
}

function evidenceWord(evidence: Evidence) {
  switch (evidence) {
    case Evidence.NOT_GRANTED:
      return "not granted"
    case Evidence.NOT_RETAINED:
      return "not retained"
    default:
      return "unreadable"
  }
}

/** Everything the response kept, newest first, with what it no longer keeps marked. */
export function evidence(response: GetDiagnosticsResponse): TimelineEvent[] {
  const rows: TimelineEvent[] = eventsOf(response.events, "the resource", "owner")
  for (const job of response.jobs) {
    if (job.evidence !== Evidence.SHOWN) {
      rows.push({
        key: `job-${job.name}`,
        time: "–",
        tone: "off",
        title: `Job ${job.name}: ${evidenceWord(job.evidence)}`,
        detail: job.problem,
      })
      continue
    }
    const state = job.state
    const failed = (state?.failed ?? 0) > 0
    rows.push({
      key: `job-${job.name}`,
      time: clock(state?.completedAt ?? state?.startedAt),
      tone: failed ? "bad" : (state?.succeeded ?? 0) > 0 ? "ok" : "run",
      title: `Job ${job.name}`,
      detail: `${job.namedBy} · ${state?.succeeded ?? 0} succeeded, ${state?.failed ?? 0} failed, ${state?.active ?? 0} active`,
    })
    rows.push(...eventsOf(job.events, `Job ${job.name}`, `job-${job.name}`))
    if (job.podsEvidence !== Evidence.SHOWN) {
      rows.push({
        key: `pods-${job.name}`,
        time: "–",
        tone: "off",
        title: `Pods of ${job.name}: ${evidenceWord(job.podsEvidence)}`,
        detail: job.podsProblem,
      })
    }
    for (const pod of job.pods) {
      const exit = pod.containers.find((c) => c.state === "terminated")
      rows.push({
        key: `pod-${pod.name}`,
        time: "",
        tone: pod.phase === "Failed" ? "bad" : pod.phase === "Succeeded" ? "ok" : "run",
        title: `Pod ${pod.name} · ${pod.phase}`,
        detail: exit
          ? `${exit.name} exited ${exit.exitCode}${exit.reason ? ` (${exit.reason})` : ""}`
          : pod.message,
      })
      rows.push(...eventsOf(pod.events, `Pod ${pod.name}`, `pod-${pod.name}`))
    }
  }
  return rows
}

function lastLog(response: GetDiagnosticsResponse) {
  for (const job of response.jobs) {
    for (const pod of job.pods) {
      const containers = [...pod.containers].sort(
        (a, b) => Number(b.exitCode !== 0) - Number(a.exitCode !== 0)
      )
      for (const container of containers) {
        if (container.log?.evidence === Evidence.SHOWN && container.log.text) {
          return {
            where: `${pod.name} / ${container.name}`,
            text: container.log.text,
            truncated: container.log.truncated,
          }
        }
      }
    }
  }
  return undefined
}

function AcknowledgeDialog({
  resource,
  context,
  onClose,
}: {
  resource: ResourceView
  context?: string
  onClose: () => void
}) {
  const session = useSession()
  const transport = useTransport()
  const queryClient = useQueryClient()
  const acknowledge = useMutation(DecisionService.method.acknowledgeMigrationRun)
  const run = resource.unresolvedRun!

  async function send() {
    const response = await acknowledge.mutateAsync({
      namespace: resource.namespace,
      migrationName: resource.name,
      migrationUid: resource.ref.uid,
      operationId: run.operationId,
    })
    void queryClient.invalidateQueries()
    return response.result!
  }

  async function readBack(name: string) {
    try {
      return (
        await createClient(DecisionService, transport).getDecision({
          kind: DecisionKind.RUN_ACKNOWLEDGMENT,
          namespace: resource.namespace,
          name,
        })
      ).decision
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
      title={`Acknowledge run ${run.operationId}?`}
      explanation={`Settles this one operation (${run.outcome}). It is not evidence that the SQL effect was verified.`}
      needsReview
      ready
      send={send}
      readBack={readBack}
      admittedTitle="Acknowledgment admitted"
      admittedText={(decision) =>
        `Recorded by admission as ${decision?.decidedBy ?? "you"}. The operator continues from a fresh history read.`
      }
    />
  )
}

export function decidedAt(at: Parameters<typeof timestampDate>[0] | undefined) {
  return at ? timestampDate(at).toLocaleString() : "–"
}
