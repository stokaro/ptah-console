import { Card, Eyebrow, StatusGlyph, type Tone } from "@ptah/console-ui"
import type { ReactNode } from "react"

import { useInfo, useScope, useSession, type Capability } from "../../api/resources"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import { Access, Answer, type NamespaceScope } from "../../gen/ptah/console/v1/resources_pb"
import { Loading, ReadError } from "../common"
import { Facts } from "./detail"

/**
 * The questions the server asks the API server for each namespace, grouped
 * by what they let a person do. The order and the Kubernetes words mirror
 * capabilityChecks in internal/api/scope.go.
 */
const groups: { title: string; rows: { capability: Capability; label: string; asks: string }[] }[] =
  [
    {
      title: "Read",
      rows: [
        { capability: "listSchemas", label: "List schemas", asks: "list ptahschemas" },
        { capability: "listMigrations", label: "List migrations", asks: "list ptahmigrations" },
        { capability: "readPlanSql", label: "Read a plan's SQL", asks: "get ptahschemaplanchunks" },
        { capability: "listDecisions", label: "List decisions", asks: "list ptahschemaapprovals" },
      ],
    },
    {
      title: "Decide",
      rows: [
        {
          capability: "approveSchemaPlans",
          label: "Approve schema plans",
          asks: "create ptahschemaapprovals",
        },
        {
          capability: "approveMigrationPlans",
          label: "Approve migration plans",
          asks: "create ptahmigrationapprovals",
        },
        {
          capability: "acknowledgeRuns",
          label: "Acknowledge runs",
          asks: "create ptahmigrationrunacknowledgments",
        },
      ],
    },
    {
      title: "Change",
      rows: [
        { capability: "suspendSchemas", label: "Suspend schemas", asks: "patch ptahschemas" },
        {
          capability: "suspendMigrations",
          label: "Suspend migrations",
          asks: "patch ptahmigrations",
        },
      ],
    },
    {
      title: "Diagnose",
      rows: [
        { capability: "readJobs", label: "Read Jobs", asks: "get jobs" },
        { capability: "readPods", label: "List Pods", asks: "list pods" },
        { capability: "readLogs", label: "Read logs", asks: "get pods/log" },
        { capability: "readEvents", label: "List Events", asks: "list events" },
      ],
    },
  ]

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

/**
 * Before acting: what this identity may do in each namespace, whether the
 * cluster serves the operator's API, which operator the console was built
 * against, and what the console never reads whatever RBAC allows.
 */
export function Preflight() {
  const scope = useScope()
  const info = useInfo()
  const session = useSession()
  if (scope.isPending) {
    return <Loading what="what this identity may do" />
  }
  if (scope.error) {
    return <ReadError error={scope.error} what="what this identity may do" />
  }
  const api = scope.data?.operatorApi
  const identity = session.data
  return (
    <>
      <section className="flex flex-col gap-1.5">
        <Eyebrow>Workspace</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Pre-flight
        </h1>
        <span className="text-[13.5px] text-fg2">
          What this identity may do in each namespace, as the API server answers it. The answers
          decide what a page offers; the request itself is what the API server decides.
        </span>
      </section>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card title="Session">
          <Facts
            rows={[
              [
                "Acts as",
                <b key="u" className="font-mono">
                  {identity?.kubernetesUsername ?? "–"}
                </b>,
              ],
              [
                "Through",
                identity
                  ? identity.mode === Mode.LOCAL
                    ? `the kubeconfig, context ${identity.context}`
                    : "impersonation by the hosted console"
                  : "–",
              ],
              [
                "Decisions",
                identity?.readOnly
                  ? "Refused: this console runs read-only"
                  : "Sent as this identity",
              ],
            ]}
          />
        </Card>
        <Card title="Operator">
          <Facts
            rows={[
              [
                "API",
                api?.served && api.missing.length === 0 ? (
                  <span key="a" className="inline-flex items-center gap-1.5">
                    <StatusGlyph tone="ok" size={20} />
                    operator.ptah.run/v1alpha1 is served, with every resource the console uses
                  </span>
                ) : (
                  <span key="a" className="inline-flex items-center gap-1.5">
                    <StatusGlyph tone="bad" size={20} />
                    {api?.problem || "Not known"}
                  </span>
                ),
              ],
              ...(api?.missing.length
                ? ([
                    [
                      "Not served",
                      <code key="m" className="font-mono text-xs">
                        {api.missing.join(", ")}
                      </code>,
                    ],
                  ] as [string, ReactNode][])
                : []),
              [
                "Built against",
                info.data?.operatorVersion ? (
                  <code key="v" className="font-mono text-xs">
                    Ptah Operator {info.data.operatorVersion}
                  </code>
                ) : (
                  "not recorded"
                ),
              ],
            ]}
          />
          <span className="text-xs text-mute">
            The console does not ask the cluster which operator runs. Each action is judged on the
            object it concerns: a schema plan is approved only when its contract version is one this
            console reads.
          </span>
        </Card>
      </div>
      <Card title="What this identity may do">
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-[13px]">
            <caption className="sr-only">What this identity may do, by namespace</caption>
            <thead>
              <tr className="border-b border-line">
                <th
                  scope="col"
                  className="py-2 pr-3 font-mono text-[10.5px] font-medium tracking-[0.14em] text-mute uppercase"
                >
                  Action
                </th>
                {scope.scopes.map((ns) => (
                  <th
                    key={ns.name}
                    scope="col"
                    className="px-3 py-2 font-mono text-xs font-semibold"
                  >
                    {ns.name}
                  </th>
                ))}
              </tr>
            </thead>
            {groups.map((group) => (
              <tbody key={group.title}>
                <tr>
                  <th
                    scope="colgroup"
                    colSpan={scope.scopes.length + 1}
                    className="pt-3 pb-1 text-left font-mono text-[10px] font-medium tracking-[0.16em] text-mute uppercase"
                  >
                    {group.title}
                  </th>
                </tr>
                {group.rows.map((row) => (
                  <tr key={row.capability} className="border-b border-line last:border-b-0">
                    <th scope="row" className="py-2 pr-3 text-left font-medium">
                      {row.label}
                      <code className="block font-mono text-[11px] font-normal text-mute">
                        {row.asks}
                      </code>
                    </th>
                    {scope.scopes.map((ns) => {
                      const answer = answerOf(ns, row.capability)
                      return (
                        <td key={ns.name} className="px-3 py-2">
                          <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                            <StatusGlyph tone={answer.tone} size={20} />
                            {answer.word}
                          </span>
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            ))}
          </table>
        </div>
        <span className="text-xs text-mute">
          Unknown means the API server did not answer the question, and the console treats it as not
          allowed. A namespace this identity may not read shows no access throughout.
        </span>
      </Card>
      <Card title="Never read">
        <span className="text-sm">
          Whatever RBAC allows, the console never reads Secrets, PtahResultRecord objects,
          ConfigMaps, PtahRealm objects or Leases, and never writes status. A target is shown by the
          name and key of its Secret, never by what the Secret holds; a plan&apos;s SQL is read only
          from its chunks, through the operator&apos;s own integrity checks.
        </span>
      </Card>
    </>
  )
}
