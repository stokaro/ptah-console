import { Button, Card, copyWords, Eyebrow, StatusGlyph, useCopy, type Tone } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { useState } from "react"

import { useLive } from "../../api/live"
import { useInfo, useResources, useScope, useSession, type Capability } from "../../api/resources"
import { Access, Answer, type NamespaceScope } from "../../gen/ptah/console/v1/resources_pb"
import { punctuality } from "../../model/operator"
import {
  apiCheck,
  kubectlCommand,
  kubernetesCheck,
  liveCheck,
  namespacesCheck,
  scheduleCheck,
  sessionCheck,
  type Check,
} from "../../model/preflight"
import { matching } from "../../model/scope"
import { Loading, ReadError, useNow } from "../common"

interface Column {
  capability: Capability
  /** The whole question, as a cell's accessible name and the legend say it. */
  label: string
  /** The column heading, short enough for thirteen columns. */
  short: string
  /** The question in the API server's words. */
  asks: string
}

/**
 * The questions the server asks the API server for each namespace, grouped
 * by what they let a person do. The order and the Kubernetes words mirror
 * capabilityChecks in internal/api/scope.go.
 */
const groups: { title: string; columns: Column[] }[] = [
  {
    title: "Read",
    columns: [
      {
        capability: "listSchemas",
        label: "List schemas",
        short: "Schemas",
        asks: "list ptahschemas",
      },
      {
        capability: "listMigrations",
        label: "List migrations",
        short: "Migrations",
        asks: "list ptahmigrations",
      },
      {
        capability: "readPlanSql",
        label: "Read a plan's SQL",
        short: "Plan SQL",
        asks: "get ptahschemaplanchunks",
      },
      {
        capability: "listDecisions",
        label: "List decisions",
        short: "Decisions",
        asks: "list ptahschemaapprovals",
      },
    ],
  },
  {
    title: "Decide",
    columns: [
      {
        capability: "approveSchemaPlans",
        label: "Approve schema plans",
        short: "Schema plans",
        asks: "create ptahschemaapprovals",
      },
      {
        capability: "approveMigrationPlans",
        label: "Approve migration plans",
        short: "Migration plans",
        asks: "create ptahmigrationapprovals",
      },
      {
        capability: "acknowledgeRuns",
        label: "Acknowledge runs",
        short: "Runs",
        asks: "create ptahmigrationrunacknowledgments",
      },
    ],
  },
  {
    title: "Change",
    columns: [
      {
        capability: "suspendSchemas",
        label: "Suspend schemas",
        short: "Schemas",
        asks: "patch ptahschemas",
      },
      {
        capability: "suspendMigrations",
        label: "Suspend migrations",
        short: "Migrations",
        asks: "patch ptahmigrations",
      },
    ],
  },
  {
    title: "Diagnose",
    columns: [
      { capability: "readJobs", label: "Read Jobs", short: "Jobs", asks: "get jobs" },
      { capability: "readPods", label: "List Pods", short: "Pods", asks: "list pods" },
      { capability: "readLogs", label: "Read logs", short: "Logs", asks: "get pods/log" },
      { capability: "readEvents", label: "List Events", short: "Events", asks: "list events" },
    ],
  },
]

/** The most namespaces the matrix shows at once; a filter narrows the rest. */
export const matrixRows = 25

const answers: Record<number, { tone: Tone; word: string }> = {
  [Answer.ALLOWED]: { tone: "ok", word: "Allowed" },
  [Answer.DENIED]: { tone: "off", word: "Denied" },
  [Answer.UNKNOWN]: { tone: "unk", word: "Unknown" },
}

/** How an answer reads; anything the server could not establish is unknown, never allowed. */
export function answerOf(scope: NamespaceScope, capability: Capability) {
  if (scope.access !== Access.OK) {
    return { tone: "off" as Tone, word: "No access" }
  }
  return answers[scope.capabilities?.[capability] ?? Answer.UNKNOWN] ?? answers[Answer.UNKNOWN]
}

const toneWord: Record<Tone, string> = {
  ok: "passed",
  warn: "warning",
  bad: "failed",
  unk: "unknown",
  run: "in progress",
  off: "not judged",
}

/**
 * Before acting: whether the operator answers, whether this console fits the
 * cluster, and what this identity may do in each namespace, as the API server
 * says it. The answers decide what a page offers; a request is still decided
 * by the API server.
 */
export function Preflight() {
  const scope = useScope()
  const info = useInfo()
  const session = useSession()
  const live = useLive()
  const { resources } = useResources()
  const now = useNow(30_000)
  const queryClient = useQueryClient()
  const [copyState, copy] = useCopy()
  const [filter, setFilter] = useState("")
  if (scope.isPending) {
    return <Loading what="what this identity may do" />
  }
  if (scope.error) {
    return <ReadError error={scope.error} what="what this identity may do" />
  }
  const identity = session.isSuccess ? session.data : undefined
  const tested = info.data?.kubernetesMinors ?? []
  const clusterVersion = scope.data?.kubernetesVersion ?? ""
  const checks: Check[] = [
    apiCheck(scope.data?.operatorApi),
    kubernetesCheck(clusterVersion, tested),
    sessionCheck(identity),
    namespacesCheck(scope.scopes, scope.data?.truncated ?? false, scope.data?.maxNamespaces ?? 0),
    liveCheck(live, scope.readable.length > 0),
    scheduleCheck(punctuality(resources, now)),
  ]
  const matched = new Set(
    matching(
      scope.scopes.map((ns) => ns.name),
      filter
    )
  )
  const rows = scope.scopes.filter((ns) => matched.has(ns.name))
  const shown = rows.slice(0, matrixRows)
  const command = kubectlCommand(
    identity,
    shown.map((ns) => ns.name)
  )
  const checkedAt = scope.dataUpdatedAt ? new Date(scope.dataUpdatedAt) : undefined

  return (
    <>
      <section className="flex flex-wrap items-end justify-between gap-3">
        <span className="flex flex-col gap-1.5">
          <Eyebrow>Installation</Eyebrow>
          <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
            Pre-flight
          </h1>
          <span className="max-w-[720px] text-[13.5px] text-fg2">
            Whether the operator answers, whether this console fits the cluster, and what this
            identity may do in each namespace, as the API server says it.
          </span>
        </span>
        <span className="flex flex-wrap items-center gap-2">
          <span role="status" className="text-[12.5px] text-mute">
            {copyWords[copyState]}
          </span>
          <Button onClick={() => void copy(command)} title={command}>
            Copy kubectl command
          </Button>
          <Button onClick={() => void copy(window.location.href)}>Copy link</Button>
          <Button onClick={() => void queryClient.invalidateQueries()} disabled={scope.isFetching}>
            {scope.isFetching ? "Checking…" : "Re-run checks"}
          </Button>
        </span>
      </section>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
        <Card title="Checks">
          <ul aria-label="Checks" className="m-0 flex list-none flex-col gap-3 p-0">
            {checks.map((check) => (
              <li key={check.title} className="flex items-start gap-2.5 text-[13px]">
                <StatusGlyph tone={check.tone} size={20} label={toneWord[check.tone]} />
                <span className="flex min-w-0 flex-col">
                  <b className="font-semibold">{check.title}</b>
                  <span className="text-fg2">{check.detail}</span>
                </span>
              </li>
            ))}
          </ul>
          {checkedAt ? (
            <span className="text-xs text-mute">
              Asked at {checkedAt.toLocaleTimeString()}. Re-run asks every question again.
            </span>
          ) : null}
        </Card>

        <Card title="What this identity may do">
          <span className="text-xs text-mute">
            SelfSubjectAccessReview as{" "}
            <b className="font-mono font-semibold">{identity?.kubernetesUsername ?? "–"}</b>, one
            question per action and namespace.
          </span>
          <label className="flex h-9 items-center gap-2 rounded-[10px] border border-line bg-bg px-2.5 text-mute">
            <span aria-hidden="true">⌕</span>
            <input
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder={`Filter ${scope.scopes.length} ${scope.scopes.length === 1 ? "namespace" : "namespaces"}…`}
              aria-label="Filter namespaces"
              className="min-w-0 flex-auto border-0 bg-transparent text-sm text-fg outline-0"
            />
          </label>
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-left text-[13px]">
              <caption className="sr-only">What this identity may do, by namespace</caption>
              <thead>
                <tr>
                  <td />
                  {groups.map((group) => (
                    <th
                      key={group.title}
                      scope="colgroup"
                      colSpan={group.columns.length}
                      className="border-l border-line px-2 pt-1 font-mono text-[10px] font-medium tracking-[0.16em] text-mute uppercase"
                    >
                      {group.title}
                    </th>
                  ))}
                </tr>
                <tr className="border-b border-line">
                  <th scope="col" className="py-2 pr-3 text-xs font-medium text-mute">
                    Namespace
                  </th>
                  {groups.flatMap((group) =>
                    group.columns.map((column, index) => (
                      <th
                        key={column.capability}
                        scope="col"
                        title={column.asks}
                        className={`px-2 py-2 text-[11px] font-medium whitespace-nowrap text-fg2 ${index === 0 ? "border-l border-line" : ""}`}
                      >
                        {column.short}
                      </th>
                    ))
                  )}
                </tr>
              </thead>
              <tbody>
                {shown.map((ns) => (
                  <tr key={ns.name} className="border-b border-line last:border-b-0">
                    <th scope="row" className="py-2 pr-3 text-left font-mono text-xs font-semibold">
                      {ns.name}
                      {ns.access !== Access.OK ? (
                        <span className="block font-sans text-[11px] font-normal text-mute">
                          {ns.reason || "no access"}
                        </span>
                      ) : null}
                    </th>
                    {groups.flatMap((group) =>
                      group.columns.map((column, index) => {
                        const answer = answerOf(ns, column.capability)
                        return (
                          <td
                            key={column.capability}
                            title={`${column.label}: ${answer.word}`}
                            className={`px-2 py-2 ${index === 0 ? "border-l border-line" : ""}`}
                          >
                            <StatusGlyph
                              tone={answer.tone}
                              size={20}
                              label={`${column.label}: ${answer.word}`}
                            />
                          </td>
                        )
                      })
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {shown.length === 0 ? (
            <p className="m-0 text-sm text-fg2">No namespace matches “{filter.trim()}”.</p>
          ) : null}
          {rows.length > shown.length ? (
            <span className="text-xs text-mute">
              Showing {shown.length} of {rows.length} · refine the filter
            </span>
          ) : null}
          <ul
            aria-label="What each column asks"
            className="m-0 grid list-none gap-x-4 gap-y-1 p-0 text-xs text-fg2 sm:grid-cols-2"
          >
            {groups.flatMap((group) =>
              group.columns.map((column) => (
                <li key={column.capability}>
                  <span className="text-mute">{group.title} ·</span> {column.label}:{" "}
                  <code className="font-mono text-[11px]">{column.asks}</code>
                </li>
              ))
            )}
          </ul>
          <span className="text-xs text-mute">
            Unknown means the API server did not answer, and the console treats it as not allowed.
            Secrets are never read by the console, whatever RBAC allows.
          </span>
        </Card>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Compatibility">
          <table className="w-full border-collapse text-left text-[13px]">
            <caption className="sr-only">What this console was built and tested against</caption>
            <tbody>
              <Fact
                name="This console"
                value={info.data?.version ?? "–"}
                about={
                  info.data?.commit
                    ? `commit ${info.data.commit.slice(0, 12)}`
                    : "commit not recorded"
                }
              />
              <Fact
                name="Built against"
                value={
                  info.data?.operatorVersion
                    ? `Ptah Operator ${info.data.operatorVersion}`
                    : "not recorded"
                }
                about="the API types and the plan reader the console uses"
              />
              <Fact
                name="Plan contracts"
                value={
                  info.data && info.data.planContractVersions.length > 0
                    ? info.data.planContractVersions.join(", ")
                    : "–"
                }
                about="the schema plans it reads and approves; a plan of another contract is shown without its SQL"
              />
              <Fact
                name="Tested on"
                value={tested.length > 0 ? `Kubernetes ${tested.join(", ")}` : "not recorded"}
                about={
                  clusterVersion
                    ? `this cluster runs ${clusterVersion}`
                    : "this cluster did not say its version"
                }
              />
            </tbody>
          </table>
          <span className="text-xs text-mute">
            The console does not ask the cluster which operator runs. Each action is judged on the
            object it concerns, and anything the console cannot establish is shown as unknown.
          </span>
        </Card>
        <Card title="Never read">
          <span className="text-sm">
            Whatever RBAC allows, the console never reads Secrets, PtahResultRecord objects,
            ConfigMaps, PtahRealm objects or Leases, and never writes status. A target is shown by
            the name and key of its Secret, never by what the Secret holds; a plan&apos;s SQL is
            read only from its chunks, through the operator&apos;s own integrity checks.
          </span>
        </Card>
      </div>
    </>
  )
}

function Fact({ name, value, about }: { name: string; value: string; about: string }) {
  return (
    <tr className="border-b border-line align-top last:border-b-0">
      <th scope="row" className="py-2 pr-3 text-left font-medium text-mute">
        {name}
      </th>
      <td className="py-2">
        <code className="font-mono text-xs font-semibold">{value}</code>
        <span className="block text-xs text-fg2">{about}</span>
      </td>
    </tr>
  )
}
