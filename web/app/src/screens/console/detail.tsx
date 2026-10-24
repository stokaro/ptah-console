import type { Timestamp } from "@bufbuild/protobuf/wkt"
import { timestampDate } from "@bufbuild/protobuf/wkt"
import {
  Button,
  Card,
  copyWords,
  Pill,
  shortDigest,
  StatusGlyph,
  Terminal,
  useCopy,
} from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useState, type ReactNode } from "react"

import { useOffer, useSession } from "../../api/resources"
import {
  SourceKind,
  type Condition,
  type Source,
  type SourcePolicy,
  type Target,
} from "../../gen/ptah/console/v1/resources_pb"
import {
  attentionTitle,
  attentionTone,
  conditionTone,
  type ResourceView,
} from "../../model/resource"
import { SuspendDialog } from "../ReleaseDetail"

/** Facts about a resource, as a list of names and values. */
export function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="m-0 grid grid-cols-[minmax(110px,auto)_minmax(0,1fr)] gap-x-4 gap-y-2 text-[13px]">
      {rows.map(([name, value]) => (
        <div key={name} className="contents">
          <dt className="text-mute">{name}</dt>
          <dd className="m-0 min-w-0 break-words">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

/** An instant as an age, with the exact time on hover; a dash when there is none. */
export function When({ at, now }: { at?: Timestamp; now: Date }) {
  if (!at) {
    return <span className="text-mute">–</span>
  }
  const date = timestampDate(at)
  return (
    <time dateTime={date.toISOString()} title={date.toLocaleString()}>
      {relative(date, now)}
    </time>
  )
}

/** "12 min ago" for the past, "in 3 min" for a time still due. */
export function relative(date: Date, now: Date) {
  const seconds = Math.round((date.getTime() - now.getTime()) / 1000)
  const span = Math.abs(seconds)
  const unit =
    span < 60
      ? `${span} s`
      : span < 3600
        ? `${Math.floor(span / 60)} min`
        : span < 172_800
          ? `${Math.floor(span / 3600)} h`
          : `${Math.floor(span / 86_400)} d`
  return seconds > 0 ? `in ${unit}` : `${unit} ago`
}

/** A digest or other long value, in monospace, shortened, whole on hover. */
export function Mono({ value, short = false }: { value: string; short?: boolean }) {
  if (!value) {
    return <span className="text-mute">–</span>
  }
  return (
    <code className="font-mono text-xs break-all" title={value}>
      {short ? shortDigest(value) : value}
    </code>
  )
}

export interface DetailHeaderProps {
  resource: ResourceView
  kind: "PtahSchema" | "PtahMigration"
  /** Facts shown as chips under the title. */
  chips: string[]
  /** Links and buttons beside the copy actions, such as reviewing the plan. */
  actions?: ReactNode
}

/**
 * The top of a resource's page: where it is, what it is, and the actions the
 * console offers. Suspend is offered only where RBAC says the request can
 * pass; the server decides either way.
 */
export function DetailHeader({ resource, kind, chips, actions }: DetailHeaderProps) {
  const session = useSession()
  const offer = useOffer()
  const [suspending, setSuspending] = useState(false)
  const [state, copy] = useCopy()
  const maySuspend =
    offer(
      resource.namespace,
      resource.kind === "schema" ? "suspendSchemas" : "suspendMigrations"
    ) === "allowed"
  const command = `kubectl ptah ${resource.kind} ${resource.name} -n ${resource.namespace}`
  return (
    <section className="flex flex-col gap-3">
      <nav aria-label="Breadcrumb" className="text-xs text-mute">
        <Link to="/console/resources" className="text-mute">
          Resources
        </Link>{" "}
        › {resource.namespace} › {resource.name}
      </nav>
      <h1 className="m-0 flex flex-wrap items-center gap-3 font-display text-[26px] leading-[1.1] font-semibold tracking-[-0.025em]">
        <span>
          <span className="text-mute">{kind} /</span>{" "}
          <span className="font-mono">{resource.name}</span>
        </span>
        {resource.suspended ? <Pill tone="off">Suspended</Pill> : null}
        {!resource.statusCurrent ? <Pill tone="unk">Status is stale</Pill> : null}
      </h1>
      <ul aria-label="About" className="m-0 flex list-none flex-wrap gap-1.5 p-0">
        {chips.map((chip) => (
          <li key={chip}>
            <Pill>{chip}</Pill>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-2">
        {actions}
        <Link
          to={
            resource.kind === "schema"
              ? "/console/schemas/$namespace/$name/execution"
              : "/console/migrations/$namespace/$name/execution"
          }
          params={{ namespace: resource.namespace, name: resource.name }}
          className="inline-flex min-h-10 items-center rounded-[11px] border border-line bg-card px-3.5 text-[13.5px] font-medium text-fg"
        >
          Execution &amp; diagnosis
        </Link>
        <Link
          to={
            resource.kind === "schema"
              ? "/console/schemas/$namespace/$name/history"
              : "/console/migrations/$namespace/$name/history"
          }
          params={{ namespace: resource.namespace, name: resource.name }}
          className="inline-flex min-h-10 items-center rounded-[11px] border border-line bg-card px-3.5 text-[13.5px] font-medium text-fg"
        >
          Execution and history
        </Link>
        {maySuspend ? (
          <Button onClick={() => setSuspending(true)}>
            {resource.suspended ? "Resume…" : "Suspend…"}
          </Button>
        ) : null}
        <Button onClick={() => void copy(window.location.href)}>Copy link</Button>
        <Button onClick={() => void copy(command)} title={command}>
          Copy kubectl command
        </Button>
        <span role="status" className="text-[12.5px] text-mute">
          {copyWords[state]}
        </span>
      </div>
      {suspending ? (
        <SuspendDialog
          resource={resource}
          context={session.data?.context}
          onClose={() => setSuspending(false)}
        />
      ) : null}
    </section>
  )
}

/** Why the resource needs a person, each with the step the operator supports next. */
export function AttentionCard({ resource }: { resource: ResourceView }) {
  if (resource.attention.length === 0) {
    return null
  }
  return (
    <Card title="Needs attention">
      <ul className="m-0 flex list-none flex-col gap-3 p-0">
        {resource.attention.map((attention) => (
          <li key={attention.kind} className="grid grid-cols-[24px_minmax(0,1fr)] gap-2.5">
            <StatusGlyph tone={attentionTone(attention.kind)} size={24} />
            <span className="flex flex-col gap-1 text-sm">
              <b className="font-semibold">{attentionTitle(attention.kind)}</b>
              <span>{attention.reason}</span>
              {attention.nextStep ? (
                <span className="text-fg2">Next: {attention.nextStep}</span>
              ) : null}
              {attention.command ? (
                <code className="font-mono text-xs text-fg2">{attention.command}</code>
              ) : null}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  )
}

/** Every condition the operator reports, as it last wrote it. */
export function ConditionsCard({ conditions, now }: { conditions: Condition[]; now: Date }) {
  return (
    <Card title="Conditions">
      {conditions.length === 0 ? (
        <span className="text-sm text-fg2">The operator has reported no condition yet.</span>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-[13px]">
            <caption className="sr-only">Conditions</caption>
            <thead>
              <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                <th scope="col" className="py-2 pr-3 font-medium">
                  Condition
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  Status
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  Reason
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  Message
                </th>
                <th scope="col" className="py-2 pl-3 text-right font-medium">
                  Changed
                </th>
              </tr>
            </thead>
            <tbody>
              {conditions.map((condition) => (
                <tr key={condition.type} className="border-b border-line align-top last:border-b-0">
                  <th scope="row" className="py-2 pr-3 text-left font-mono font-medium">
                    {condition.type}
                  </th>
                  <td className="px-3 py-2">
                    <span className="inline-flex items-center gap-1.5">
                      <StatusGlyph tone={conditionTone(condition)} size={20} />
                      {condition.status}
                    </span>
                  </td>
                  <td className="px-3 py-2 font-mono text-xs">{condition.reason}</td>
                  <td className="max-w-[420px] px-3 py-2 text-fg2">{condition.message}</td>
                  <td className="py-2 pl-3 text-right font-mono text-xs whitespace-nowrap text-mute">
                    <When at={condition.lastTransition} now={now} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

/**
 * The source as requested and as resolved. An OCI artifact also says whether
 * its signature was verified; a ConfigMap or a Git repository has no
 * verification policy, and says which files were read and under which source
 * policy instead.
 */
export function ArtifactCard({ source, now }: { source?: Source; now: Date }) {
  if (source?.kind === SourceKind.GIT || source?.kind === SourceKind.CONFIG_MAP) {
    const git = source.kind === SourceKind.GIT
    return (
      <Card title="Source">
        <Facts
          rows={[
            ...(git
              ? ([
                  ["Repository", <Mono key="r" value={source.requested} />],
                  ["Ref", <Mono key="f" value={source.ref} />],
                  ["Path", <Mono key="p" value={source.path} />],
                  ["Commit", <Mono key="c" value={source.resolved} />],
                ] satisfies [string, ReactNode][])
              : ([
                  ["ConfigMap", <Mono key="r" value={source.requested} />],
                  ["Snapshot", <Mono key="s" value={source.resolved} />],
                ] satisfies [string, ReactNode][])),
            ["Content", <Mono key="d" value={source.contentDigest} />],
            ["Source policy", <SourcePolicyFact key="p" policy={source.policy} />],
            ["Resolved at", <When key="a" at={source.resolvedAt} now={now} />],
          ]}
        />
      </Card>
    )
  }
  return (
    <Card title="Artifact">
      <Facts
        rows={[
          ["Requested", <Mono key="r" value={source?.requested ?? ""} />],
          ["Resolved", <Mono key="s" value={source?.resolved ?? ""} />],
          ["Digest", <Mono key="d" value={source?.digest ?? ""} />],
          [
            "Verified",
            source?.verified ? (
              <span key="v" className="inline-flex items-center gap-1.5">
                <StatusGlyph tone="ok" size={20} /> Yes, <When at={source.verifiedAt} now={now} />
              </span>
            ) : (
              <span key="v" className="inline-flex items-center gap-1.5">
                <StatusGlyph tone="off" size={20} /> Not verified
              </span>
            ),
          ],
          ["Resolved at", <When key="a" at={source?.resolvedAt} now={now} />],
        ]}
      />
    </Card>
  )
}

/**
 * The source policy a source names, and the digest of the one its last
 * resolution was checked against. A policy named and not read yet has no
 * digest.
 */
function SourcePolicyFact({ policy }: { policy?: SourcePolicy }) {
  if (!policy) {
    return <span className="text-mute">None</span>
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      {policy.name || <span className="text-mute">no longer named</span>}
      {policy.digest ? (
        <Mono value={policy.digest} short />
      ) : (
        <span className="text-mute">not read yet</span>
      )}
    </span>
  )
}

/**
 * The database by reference: its engine and the Secret and key that hold its
 * address, never the address. Drift is what the last read of it found.
 */
export function TargetCard({ target, now }: { target?: Target; now: Date }) {
  const findings = target?.driftFindings ?? []
  return (
    <Card title="Target">
      <Facts
        rows={[
          ["Engine", target?.engine || <span className="text-mute">–</span>],
          [
            "Address from",
            target?.secretName ? (
              <span>
                Secret <code className="font-mono text-xs">{target.secretName}</code>, key{" "}
                <code className="font-mono text-xs">{target.secretKey}</code>
              </span>
            ) : (
              <span className="text-mute">–</span>
            ),
          ],
          ["Realm", target?.realm || <span className="text-mute">none</span>],
          ["Last read", <When key="o" at={target?.lastObservedAt} now={now} />],
        ]}
      />
      {target && target.driftFindingCount > 0 ? (
        <div className="flex flex-col gap-2">
          <span className="text-sm">
            The last read found {target.driftFindingCount}{" "}
            {target.driftFindingCount === 1 ? "difference" : "differences"} from the artifact
            {target.highestDriftSeverity ? `, the most severe ${target.highestDriftSeverity}` : ""}.
          </span>
          <table className="w-full border-collapse text-left text-[13px]">
            <caption className="sr-only">Drift by category</caption>
            <thead>
              <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                <th scope="col" className="py-1.5 pr-3 font-medium">
                  Category
                </th>
                <th scope="col" className="px-3 py-1.5 font-medium">
                  Severity
                </th>
                <th scope="col" className="py-1.5 pl-3 text-right font-medium">
                  Count
                </th>
              </tr>
            </thead>
            <tbody>
              {findings.map((finding) => (
                <tr
                  key={`${finding.category}/${finding.severity}`}
                  className="border-b border-line last:border-b-0"
                >
                  <th scope="row" className="py-1.5 pr-3 text-left font-mono font-medium">
                    {finding.category}
                  </th>
                  <td className="px-3 py-1.5">{finding.severity}</td>
                  <td className="py-1.5 pl-3 text-right font-mono">{finding.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : target?.lastObservedAt ? (
        <span className="text-sm text-fg2">
          The last read found no difference from the artifact.
        </span>
      ) : null}
    </Card>
  )
}

/**
 * The spec as the API server stores it, read-only. "Copy as patch" copies a
 * kubectl command that sets these fields back; the console sends nothing.
 */
export function SpecCard({ resource, spec }: { resource: ResourceView; spec: string }) {
  const [state, copy] = useCopy()
  const resourceType = resource.kind === "schema" ? "ptahschema" : "ptahmigration"
  const patch = `kubectl -n ${resource.namespace} patch ${resourceType} ${resource.name} --type merge --patch-file /dev/stdin <<'YAML'
spec:
${spec
  .trimEnd()
  .split("\n")
  .map((line) => `  ${line}`)
  .join("\n")}
YAML`
  return (
    <Card title="Spec">
      <span className="text-[13px] text-fg2">
        Read-only. The console writes nothing from it: change the spec where it is managed, or run
        the copied command yourself. A merge patch sets the fields shown and leaves a field added
        since in place.
      </span>
      {spec ? (
        <>
          <Terminal label={`Spec of ${resource.name}`} text={spec} />
          <div className="flex flex-wrap items-center gap-2">
            <Button onClick={() => void copy(spec)}>Copy YAML</Button>
            <Button onClick={() => void copy(patch)}>Copy as patch</Button>
            <span role="status" className="text-[12.5px] text-mute">
              {copyWords[state]}
            </span>
          </div>
        </>
      ) : (
        <span className="text-sm text-mute">The spec could not be shown.</span>
      )}
    </Card>
  )
}
