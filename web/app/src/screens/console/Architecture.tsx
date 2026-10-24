import { Eyebrow } from "@ptah/console-ui"
import type { ReactNode } from "react"

/** Where the operator's own documentation explains each part in full. */
const docs = {
  architecture: "https://operator.ptah.run/reference/architecture/",
  security: "https://operator.ptah.run/use/security/",
  reconciliation: "https://operator.ptah.run/reference/reconciliation/",
}

const flow: { name: string; text: string }[] = [
  {
    name: "Source",
    text: "An OCI artifact pinned by digest and verified by policy, a snapshot of a ConfigMap, or a Git commit.",
  },
  { name: "Resolve", text: "The operator pins what the source names before anything reads it." },
  {
    name: "Observe",
    text: "A read-only Job reads the database: its schema, or its revision table.",
  },
  {
    name: "Plan",
    text: "Ptah computes the exact change. The operator stores it immutable, in chunks it checks on every read.",
  },
  {
    name: "Approve",
    text: "A person approves that exact plan as themselves. Admission records who.",
  },
  {
    name: "Apply",
    text: "A Job runs the approved plan and nothing else, holding the database's lock.",
  },
  {
    name: "Verify",
    text: "A fresh read-only observation proves the result. A Job's exit code proves nothing.",
  },
]

const lifecycles: { kind: string; verb: string; steps: string[] }[] = [
  {
    kind: "PtahSchema",
    verb: "converges",
    steps: ["Resolve", "Verify", "Observe", "Plan", "Approval", "Apply", "Verify convergence"],
  },
  {
    kind: "PtahMigration",
    verb: "advances",
    steps: ["Resolve", "Verify", "Read history", "Plan", "Approval", "Apply", "Verify history"],
  },
]

const properties: { name: string; text: string }[] = [
  {
    name: "The exact plan",
    text: "An approval names the plan's UID and fingerprint. A plan that changed needs a new approval.",
  },
  {
    name: "No blind retry, no rollback",
    text: "An Apply whose outcome is unknown is settled by reading the database, never by running it again. A migration run that may have committed part of a file waits for a person.",
  },
  {
    name: "Proof, not exit codes",
    text: "Convergence is what a new observation shows after the Apply, not what the Apply Job reported.",
  },
  {
    name: "One writer per database",
    text: "Every resource that writes to the same database takes its turn under one lock.",
  },
  {
    name: "Credentials kept apart",
    text: "A Job gets only the credentials its one operation needs. The process that runs SQL never holds a registry or Git credential, and the operator's manager reads no Secrets.",
  },
  {
    name: "Who may skip an approval",
    text: "Under apply: Always, a plan that destroys nothing and changes no privilege runs without one. An installation can refuse an approval from the person who last changed the resource.",
  },
]

const semantics: { name: string; values: [string, string] }[] = [
  {
    name: "Declares",
    values: ["The schema the database should have", "An ordered sequence of migration files"],
  },
  {
    name: "Goal",
    values: ["Converge: no difference left", "Advance: every version applied, in order, once"],
  },
  {
    name: "Plan",
    values: [
      "The SQL from what the database is to what the schema declares",
      "The next versions to apply, against the history it read",
    ],
  },
  {
    name: "Evidence",
    values: [
      "The drift the last observation found and the plan it applied",
      "The revision table, as the last history read summarized it",
    ],
  },
]

const boundaries: { name: string; text: string }[] = [
  {
    name: "This console",
    text: "A typed client acting as you. It never reads Secrets, result records or arbitrary ConfigMaps, and decides nothing.",
  },
  {
    name: "The operator's manager",
    text: "Decides and records. It alone writes status, and its writes are held to fixed shapes by admission.",
  },
  {
    name: "Operation Jobs",
    text: "Run Ptah for one operation each, with the database credential that operation needs.",
  },
  {
    name: "The API server",
    text: "RBAC and the operator's admission decide every request, this console's included.",
  },
]

/**
 * How Ptah Operator works and what it holds itself to, in the order a
 * newcomer reads it. Nothing here is read from the cluster: it describes the
 * operator, not this installation.
 */
export function Architecture() {
  return (
    <>
      <section className="flex flex-col gap-1.5">
        <Eyebrow>Installation</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Architecture &amp; safety
        </h1>
        <span className="max-w-[720px] text-[13.5px] text-fg2">
          How the operator turns a declared change into a database change, the order it follows, and
          the guarantees on the way. The{" "}
          <a href={docs.architecture} className="font-medium text-pri">
            architecture
          </a>
          ,{" "}
          <a href={docs.reconciliation} className="font-medium text-pri">
            reconciliation
          </a>{" "}
          and{" "}
          <a href={docs.security} className="font-medium text-pri">
            security
          </a>{" "}
          pages say it in full.
        </span>
      </section>

      <Panel
        title="System map"
        sub="From a declared change to a proven database, one step at a time"
      >
        <ol className="m-0 grid list-none gap-2 p-0 sm:grid-cols-2 lg:grid-cols-7">
          {flow.map((step, index) => (
            <li
              key={step.name}
              className="flex flex-col gap-1 rounded-xl border border-line bg-bg p-3 text-[13px]"
            >
              <span className="font-mono text-[11px] text-mute">{index + 1}</span>
              <b className="font-semibold">{step.name}</b>
              <span className="text-fg2">{step.text}</span>
            </li>
          ))}
        </ol>
        <p className="m-0 text-[13px] text-fg2">
          Each step reports back through the resource: its status, its conditions, and the Events
          Kubernetes keeps for about an hour.
        </p>
      </Panel>

      <Panel title="Operator lifecycle" sub="The loop each resource repeats at its interval">
        <div className="grid gap-4 md:grid-cols-2">
          {lifecycles.map((lifecycle) => (
            <section
              key={lifecycle.kind}
              aria-label={lifecycle.kind}
              className="flex flex-col gap-2"
            >
              <h3 className="m-0 text-[13.5px] font-semibold">
                <span className="font-mono">{lifecycle.kind}</span>{" "}
                <span className="font-normal text-fg2">{lifecycle.verb}</span>
              </h3>
              <ol className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0 text-[13px]">
                {lifecycle.steps.map((step, index) => (
                  <li key={step} className="flex items-center gap-1.5">
                    <span className="rounded-lg border border-line bg-bg px-2 py-1">{step}</span>
                    {index < lifecycle.steps.length - 1 ? (
                      <span aria-hidden="true" className="text-mute">
                        →
                      </span>
                    ) : null}
                  </li>
                ))}
              </ol>
            </section>
          ))}
        </div>
      </Panel>

      <Panel title="Key safety properties" sub="What every step holds to, whoever runs it">
        <ul className="m-0 grid list-none gap-3 p-0 md:grid-cols-2 lg:grid-cols-3">
          {properties.map((property) => (
            <li key={property.name} className="flex flex-col gap-1 text-[13px]">
              <b className="font-semibold">{property.name}</b>
              <span className="text-fg2">{property.text}</span>
            </li>
          ))}
        </ul>
      </Panel>

      <div className="grid gap-4 lg:grid-cols-2">
        <Panel title="Two kinds of resource" sub="A schema converges; a migration advances">
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-left text-[13px]">
              <caption className="sr-only">
                What a PtahSchema and a PtahMigration each declare
              </caption>
              <thead>
                <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                  <td className="py-2 pr-3" />
                  <th scope="col" className="px-3 py-2 font-medium">
                    PtahSchema
                  </th>
                  <th scope="col" className="py-2 pl-3 font-medium">
                    PtahMigration
                  </th>
                </tr>
              </thead>
              <tbody>
                {semantics.map((row) => (
                  <tr key={row.name} className="border-b border-line align-top last:border-b-0">
                    <th scope="row" className="py-2 pr-3 text-left font-medium text-mute">
                      {row.name}
                    </th>
                    <td className="px-3 py-2">{row.values[0]}</td>
                    <td className="py-2 pl-3">{row.values[1]}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
        <Panel title="Security boundaries" sub="Who decides what">
          <ul className="m-0 flex list-none flex-col gap-3 p-0">
            {boundaries.map((boundary) => (
              <li key={boundary.name} className="flex flex-col gap-1 text-[13px]">
                <b className="font-semibold">{boundary.name}</b>
                <span className="text-fg2">{boundary.text}</span>
              </li>
            ))}
          </ul>
        </Panel>
      </div>
    </>
  )
}

function Panel({ title, sub, children }: { title: string; sub: string; children: ReactNode }) {
  return (
    <section
      aria-label={title}
      className="flex flex-col gap-3 rounded-[18px] border border-line bg-card p-4 shadow-card"
    >
      <span className="flex flex-col gap-0.5">
        <h2 className="m-0 text-[15px] font-semibold">{title}</h2>
        <span className="text-xs text-mute">{sub}</span>
      </span>
      {children}
    </section>
  )
}
