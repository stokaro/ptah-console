import { skipToken, useQuery } from "@connectrpc/connect-query"
import { Card, Pill, ScrollRegion, StatusGlyph, Terminal } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { can, useResource } from "../../api/resources"
import { ResourceKind } from "../../gen/ptah/console/v1/decisions_pb"
import { DiagnosticsService } from "../../gen/ptah/console/v1/diagnostics_pb"
import { coverage } from "../../model/activity"
import { lanes, originalError, timeline, type Entry, type Lane } from "../../model/execution"
import type { ResourceKind as Kind } from "../../model/resource"
import { Loading, ReadError, useNow } from "../common"
import { relative } from "./detail"
import { resourceLink } from "./links"

export interface ExecutionHistoryProps {
  kind: Kind
  namespace: string
  name: string
}

/**
 * What a resource's work left behind, on three lanes: the controller's
 * conditions and Events, the Jobs it ran with their Pods and logs, and what
 * the status records about the database. Only what the operator and
 * Kubernetes still keep is shown; what is gone is marked, not hidden.
 */
export function ExecutionHistory({ kind, namespace, name }: ExecutionHistoryProps) {
  const { resource, scope, loading, error } = useResource(kind, namespace, name)
  const now = useNow(5_000)
  const readLogs = resource ? can(scope.scopes, namespace, "readLogs") : false
  const diagnostics = useQuery(
    DiagnosticsService.method.getDiagnostics,
    resource
      ? {
          kind: kind === "schema" ? ResourceKind.SCHEMA : ResourceKind.MIGRATION,
          namespace,
          name,
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
  if (!resource) {
    return (
      <Card title="Not found">
        <span className="text-sm">
          No {kind} {name} in {namespace} that this identity may read.
        </span>
      </Card>
    )
  }
  const entries = timeline(resource, diagnostics.data)
  const kept = coverage([resource])
  const pinned = originalError(resource, diagnostics.data)
  const next = resource.attention[0]?.nextStep
  return (
    <div className="flex flex-col gap-4">
      <section className="flex flex-col gap-2">
        <nav aria-label="Breadcrumb" className="text-xs text-mute">
          <Link to="/console/resources" className="text-mute">
            Resources
          </Link>{" "}
          › {namespace} ›{" "}
          <Link {...resourceLink(resource)} className="text-mute">
            {name}
          </Link>{" "}
          › execution and history
        </nav>
        <h1 className="m-0 font-display text-[26px] leading-[1.1] font-semibold tracking-[-0.025em]">
          Execution and history of <span className="font-mono">{name}</span>
        </h1>
        <span className="text-[13.5px] text-fg2">
          The controller, the Jobs it ran and the database, on separate lanes. Only what the
          operator and Kubernetes still keep is shown; what is gone is marked, not hidden.
        </span>
      </section>
      {pinned ? (
        <section
          aria-label="Original error"
          className="flex flex-col gap-1.5 rounded-[18px] border border-[color-mix(in_oklab,var(--red)_40%,var(--line))] bg-[color-mix(in_oklab,var(--redbg)_60%,var(--card))] p-4"
        >
          <span className="font-mono text-[10px] font-medium tracking-[0.16em] text-red uppercase">
            Original error · pinned
          </span>
          <b className="text-[15px] font-semibold">{pinned.title}</b>
          {pinned.text ? <span className="text-sm">{pinned.text}</span> : null}
          {pinned.at ? <span className="text-xs text-mute">{relative(pinned.at, now)}</span> : null}
        </section>
      ) : null}
      <section className="overflow-hidden rounded-[18px] border border-line bg-card shadow-card">
        {diagnostics.isPending ? <Loading what="the evidence" /> : null}
        {diagnostics.error ? <ReadError error={diagnostics.error} what="the evidence" /> : null}
        <ScrollRegion label={`Execution and history of ${name}`}>
          <table className="w-full min-w-[720px] table-fixed border-collapse text-left text-[13px]">
            <caption className="sr-only">Execution and history of {name}</caption>
            <colgroup>
              <col className="w-[110px]" />
              <col />
              <col />
              <col />
            </colgroup>
            <thead>
              <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                <th scope="col" className="px-4 py-2.5 font-medium">
                  Time
                </th>
                {lanes.map((lane) => (
                  <th key={lane.value} scope="col" className="px-3 py-2.5 font-medium">
                    {lane.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {entries.map((entry, index) => (
                <tr
                  key={`${index}/${entry.title}`}
                  className="border-b border-line align-top last:border-b-0"
                >
                  <td className="px-4 py-2.5 font-mono text-xs text-mute">
                    {entry.at ? (
                      <time dateTime={entry.at.toISOString()} title={entry.at.toLocaleString()}>
                        {relative(entry.at, now)}
                      </time>
                    ) : (
                      "–"
                    )}
                  </td>
                  {lanes.map((lane) => (
                    <td key={lane.value} className="px-3 py-2.5">
                      {entry.lane === lane.value ? (
                        <EntryCell entry={entry} lane={lane.value} />
                      ) : null}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </ScrollRegion>
        <p className="m-0 border-t border-line px-4 py-2.5 text-xs text-mute">
          {kept.complete
            ? `The operator's journal of ${resource.name} is complete since ${kept.complete.toLocaleString()}, and keeps its latest 64 events. `
            : `The operator keeps no journal of ${resource.name}, so only what the status and Kubernetes hold now is here. `}
          Kubernetes keeps Events for about an hour, and a Job&apos;s Pods until the Job is removed.
          The status keeps each condition&apos;s last change only. &ldquo;Not retained&rdquo; means
          Kubernetes no longer keeps it, not that it never happened.
        </p>
      </section>
      <Card title="Next supported step">
        <span className="text-sm">
          {next || "Nothing to do; the operator continues on its own."}
        </span>
        <span className="text-xs text-mute">
          No blind retry and no automatic rollback: the operator offers neither.
        </span>
      </Card>
    </div>
  )
}

function EntryCell({ entry, lane }: { entry: Entry; lane: Lane }) {
  return (
    <div className="flex min-w-0 gap-2" data-lane={lane}>
      <StatusGlyph tone={entry.tone} size={20} />
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className="flex flex-wrap items-center gap-1.5 font-medium break-words">
          {entry.title}
          {entry.due ? <Pill tone="run">due</Pill> : null}
          {entry.gone ? <Pill tone="off">{entry.gone}</Pill> : null}
        </span>
        {entry.detail ? <span className="text-xs break-words text-fg2">{entry.detail}</span> : null}
        {entry.log ? (
          <details className="text-xs">
            <summary className="cursor-pointer text-pri">Log excerpt · plain text</summary>
            <Terminal label={`Log of ${entry.title}`} text={entry.log} />
          </details>
        ) : null}
      </span>
    </div>
  )
}
