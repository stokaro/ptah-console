import { Eyebrow, Segmented, StatusGlyph, type Tone } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react"

import { useResources } from "../../api/resources"
import { resourceStates, stateLabel, type ResourceState } from "../../model/catalog"
import {
  attentionGroups,
  namespaceHealth,
  needsAttention,
  recentActivity,
  stateCounts,
  type AttentionGroup,
} from "../../model/overview"
import type { NamespaceScope } from "../../gen/ptah/console/v1/resources_pb"
import { age, conditionTone, type ResourceView } from "../../model/resource"
import { Loading, ReadError, TruncatedNotice, useNow } from "../common"
import { resourceLink } from "./links"

const stateTones: Record<ResourceState, Tone> = {
  attention: "warn",
  progress: "run",
  insync: "ok",
  suspended: "off",
  none: "off",
}

const barColors: Record<ResourceState, string> = {
  attention: "bg-amber",
  progress: "bg-pri",
  insync: "bg-green",
  suspended: "bg-mute",
  none: "bg-line",
}

/** How many rows of the attention table and of the activity list the page shows. */
const attentionRows = 8
const activityRows = 8

export function Overview() {
  const { resources, scope, truncated, loading, error } = useResources()
  const now = useNow()
  if (loading) {
    return <Loading what="the resources" />
  }
  if (error) {
    return <ReadError error={error} what="the resources" />
  }
  const { counts, total } = stateCounts(resources)
  return (
    <>
      <section className="flex flex-col gap-1.5">
        <Eyebrow>Workspace</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Overview
        </h1>
        <span className="text-[13.5px] text-fg2">
          {total} {total === 1 ? "resource" : "resources"} in {scope.readable.length}{" "}
          {scope.readable.length === 1 ? "namespace" : "namespaces"} this identity may read.
        </span>
      </section>
      <TruncatedNotice namespaces={truncated} />
      <nav
        aria-label="Resources by state"
        className="grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-5"
      >
        {resourceStates.map((state) => (
          <Link
            key={state.value}
            to="/console/resources"
            search={{ state: state.value }}
            className="flex flex-col gap-2 rounded-2xl border border-line bg-card p-3.5 text-fg shadow-card hover:border-[color-mix(in_oklab,var(--pri)_40%,var(--line))]"
          >
            <span className="flex items-center gap-2 text-[13px] font-medium text-fg2">
              <StatusGlyph tone={stateTones[state.value]} size={20} />
              {state.label}
            </span>
            <span className="font-display text-[30px] leading-none font-semibold tracking-[-0.03em]">
              {counts[state.value]}
            </span>
          </Link>
        ))}
      </nav>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <AttentionSection resources={resources} now={now} />
        <div className="flex flex-col gap-4">
          <StatesSection counts={counts} total={total} />
          <NamespaceSection resources={resources} scope={scope.scopes} />
        </div>
      </div>
      <ActivitySection resources={resources} now={now} />
    </>
  )
}

function Section({ title, sub, children }: { title: string; sub?: string; children: ReactNode }) {
  return (
    <section
      aria-label={title}
      className="flex flex-col gap-3 rounded-[18px] border border-line bg-card p-4 shadow-card"
    >
      <span className="flex flex-col gap-0.5">
        <h2 className="m-0 text-[15px] font-semibold">{title}</h2>
        {sub ? <span className="text-xs text-mute">{sub}</span> : null}
      </span>
      {children}
    </section>
  )
}

function AttentionSection({ resources, now }: { resources: ResourceView[]; now: Date }) {
  const [group, setGroup] = useState<"all" | AttentionGroup>("all")
  const table = useRef<HTMLTableSectionElement>(null)
  const all = needsAttention(resources)
  const rows = all.filter((row) => group === "all" || row.group === group)
  useRowKeys(table)
  return (
    <Section title="Needs attention" sub="What a person has to look at, the most urgent first">
      <div className="flex flex-wrap items-center gap-2.5">
        <Segmented
          label="Attention"
          value={group}
          onChange={setGroup}
          options={[
            { value: "all", label: "All", count: all.length },
            ...attentionGroups
              .map((g) => ({ ...g, count: all.filter((row) => row.group === g.value).length }))
              .filter((g) => g.count > 0 || g.value === group),
          ]}
        />
        <span className="text-xs text-mute">
          <kbd className="font-mono">j</kbd> / <kbd className="font-mono">k</kbd> to move,{" "}
          <kbd className="font-mono">Enter</kbd> to open
        </span>
      </div>
      {rows.length === 0 ? (
        <p className="m-0 flex items-center gap-2.5 rounded-xl bg-[color-mix(in_oklab,var(--greenbg)_60%,var(--card))] p-3 text-sm">
          <StatusGlyph tone="ok" size={22} />
          Nothing needs a person right now.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-sm">
            <caption className="sr-only">Resources that need attention</caption>
            <thead>
              <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                <th scope="col" className="py-2 pr-3 font-medium">
                  Resource
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  Status
                </th>
                <th scope="col" className="hidden px-3 py-2 font-medium md:table-cell">
                  Reason
                </th>
                <th scope="col" className="py-2 pl-3 text-right font-medium">
                  Waiting
                </th>
              </tr>
            </thead>
            <tbody ref={table}>
              {rows.slice(0, attentionRows).map(({ resource, attention }) => {
                const state = stateLabel(resource)
                return (
                  <tr key={resource.ref.uid} className="border-b border-line last:border-b-0">
                    <td className="py-2.5 pr-3">
                      <Link
                        {...resourceLink(resource)}
                        data-attention-row
                        className="flex flex-col gap-0.5 rounded-md text-fg focus-visible:outline-2 focus-visible:outline-pri"
                      >
                        <span className="font-mono font-semibold">{resource.name}</span>
                        <span className="text-xs text-mute">
                          {resource.namespace} ·{" "}
                          {resource.kind === "schema" ? "Schema" : "Migration"}
                        </span>
                      </Link>
                    </td>
                    <td className="px-3 py-2.5">
                      <span className="inline-flex items-center gap-2 whitespace-nowrap">
                        <StatusGlyph tone={state.tone} size={20} />
                        {state.text}
                      </span>
                    </td>
                    <td className="hidden max-w-[360px] px-3 py-2.5 text-fg2 md:table-cell">
                      {attention.reason}
                    </td>
                    <td className="py-2.5 pl-3 text-right font-mono text-xs whitespace-nowrap text-mute">
                      {attention.since ? age(attention.since, now) : "–"}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
      <span className="flex flex-wrap items-center gap-3 text-xs text-mute">
        {rows.length > attentionRows
          ? `The ${attentionRows} most urgent of ${rows.length}.`
          : `${rows.length} of ${all.length} shown.`}
        <span className="flex-auto" />
        <Link
          to="/console/resources"
          search={{ state: "attention" }}
          className="font-medium text-pri"
        >
          Open all in Resources →
        </Link>
      </span>
    </Section>
  )
}

/**
 * j and k move the focus between the attention table's rows, as a mail
 * reader does; Enter then follows the focused link. Keys typed into a field,
 * or pressed with a modifier, are left alone.
 */
function useRowKeys(table: RefObject<HTMLTableSectionElement | null>) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "j" && event.key !== "k") {
        return
      }
      if (event.metaKey || event.ctrlKey || event.altKey) {
        return
      }
      const target = event.target as HTMLElement | null
      if (target?.closest("input, textarea, select, [contenteditable='true']")) {
        return
      }
      const links = [
        ...(table.current?.querySelectorAll<HTMLElement>("[data-attention-row]") ?? []),
      ]
      if (links.length === 0) {
        return
      }
      const current = links.indexOf(document.activeElement as HTMLElement)
      const next =
        current === -1
          ? event.key === "j"
            ? 0
            : links.length - 1
          : Math.min(links.length - 1, Math.max(0, current + (event.key === "j" ? 1 : -1)))
      event.preventDefault()
      links[next].focus()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [table])
}

function StatesSection({
  counts,
  total,
}: {
  counts: Record<ResourceState, number>
  total: number
}) {
  const parts = resourceStates.filter((state) => counts[state.value] > 0)
  return (
    <Section title="Resource states">
      <div
        role="img"
        aria-label={
          total === 0
            ? "No resources"
            : parts.map((state) => `${state.label}: ${counts[state.value]}`).join(", ")
        }
        className="flex h-3 overflow-hidden rounded-full bg-line"
      >
        {parts.map((state) => (
          <span
            key={state.value}
            className={barColors[state.value]}
            style={{ width: `${(counts[state.value] / total) * 100}%` }}
          />
        ))}
      </div>
      <ul className="m-0 grid list-none grid-cols-2 gap-x-4 gap-y-1.5 p-0 text-[13px]">
        {resourceStates.map((state) => (
          <li key={state.value} className="flex items-center gap-2">
            <StatusGlyph tone={stateTones[state.value]} size={20} />
            <span className="flex-auto text-fg2">{state.label}</span>
            <span className="font-mono">{counts[state.value]}</span>
          </li>
        ))}
      </ul>
    </Section>
  )
}

function NamespaceSection({
  resources,
  scope,
}: {
  resources: ResourceView[]
  scope: NamespaceScope[]
}) {
  const rows = namespaceHealth(resources, scope)
  return (
    <Section title="Namespace health" sub="Resource states by namespace">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-left text-[13px]">
          <caption className="sr-only">Resource states by namespace</caption>
          <thead>
            <tr className="border-b border-line font-mono text-[10px] tracking-[0.1em] text-mute uppercase">
              <th scope="col" className="py-2 pr-2 font-medium">
                Namespace
              </th>
              <th
                scope="col"
                className="px-1.5 py-2 text-right font-medium"
                title="Needs attention"
              >
                Attn
              </th>
              <th scope="col" className="px-1.5 py-2 text-right font-medium" title="In progress">
                Prog
              </th>
              <th scope="col" className="px-1.5 py-2 text-right font-medium" title="In sync">
                Sync
              </th>
              <th scope="col" className="px-1.5 py-2 text-right font-medium" title="Suspended">
                Susp
              </th>
              <th scope="col" className="py-2 pl-1.5 text-right font-medium">
                Total
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.namespace} className="border-b border-line last:border-b-0">
                <th
                  scope="row"
                  className="max-w-[160px] truncate py-2 pr-2 text-left font-mono font-medium"
                  title={row.namespace}
                >
                  {row.namespace}
                </th>
                {row.readable ? (
                  <>
                    <Count value={row.counts.attention} tone="text-amber" />
                    <Count value={row.counts.progress} tone="text-pri" />
                    <Count value={row.counts.insync} tone="text-green" />
                    <Count value={row.counts.suspended} tone="text-mute" />
                    <td className="py-2 pl-1.5 text-right font-mono">{row.total}</td>
                  </>
                ) : (
                  <td colSpan={5} className="py-2 pl-1.5 text-right text-xs text-mute">
                    No access
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Section>
  )
}

function Count({ value, tone }: { value: number; tone: string }) {
  return (
    <td className={`px-1.5 py-2 text-right font-mono ${value > 0 ? tone : "text-mute"}`}>
      {value}
    </td>
  )
}

function ActivitySection({ resources, now }: { resources: ResourceView[]; now: Date }) {
  const activity = recentActivity(resources, activityRows)
  return (
    <Section
      title="Recent activity"
      sub="The newest condition changes the resources still record. Each condition keeps only its last change; Kubernetes Events are on each resource's page."
    >
      {activity.length === 0 ? (
        <p className="m-0 text-sm text-fg2">No resource records a dated condition yet.</p>
      ) : (
        <ol className="m-0 flex list-none flex-col p-0">
          {activity.map(({ resource, condition, at }) => (
            <li
              key={`${resource.ref.uid}/${condition.type}`}
              className="grid grid-cols-[22px_minmax(0,1fr)_auto] items-center gap-3 border-b border-line py-2 last:border-b-0"
            >
              <StatusGlyph tone={conditionTone(condition)} size={20} />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate text-sm">
                  <Link {...resourceLink(resource)} className="font-mono font-semibold text-fg">
                    {resource.name}
                  </Link>{" "}
                  <span className="text-fg2">
                    {condition.type} {condition.status}
                    {condition.reason ? ` · ${condition.reason}` : ""}
                  </span>
                </span>
                <span className="truncate text-xs text-mute">
                  {resource.namespace}
                  {condition.message ? ` · ${condition.message}` : ""}
                </span>
              </span>
              <time
                dateTime={at.toISOString()}
                className="font-mono text-xs whitespace-nowrap text-mute"
              >
                {age(condition.lastTransition, now)} ago
              </time>
            </li>
          ))}
        </ol>
      )}
    </Section>
  )
}
