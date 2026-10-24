import { Button, Eyebrow, ScrollRegion, Segmented, StatusGlyph, type Tone } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react"

import { useDecisions, useResources } from "../../api/resources"
import {
  activity,
  coverage,
  movement,
  perDay,
  type ActivityEntry,
  type DayCount,
  type Movement,
} from "../../model/activity"
import { ago } from "../../model/connection"
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
import { namespacesRead } from "../../model/scope"
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
  progress: "bg-run",
  insync: "bg-green",
  suspended: "bg-mute",
  none: "bg-line",
}

/** How many rows of the attention table and of the activity list the page shows. */
const attentionRows = 8
const activityRows = 8
const namespaceRows = 8

export function Overview() {
  const { resources, scope, namespaces, truncated, loading, error } = useResources()
  const { decisions } = useDecisions()
  const now = useNow()
  const queryClient = useQueryClient()
  if (loading) {
    return <Loading what="the resources" />
  }
  if (error) {
    return <ReadError error={error} what="the resources" />
  }
  const { counts, total } = stateCounts(resources)
  const feed = activity(resources, decisions.data?.decisions ?? [])
  const kept = coverage(resources)
  const moved = movement(feed, kept.complete, now)
  return (
    <>
      <section className="flex flex-wrap items-end justify-between gap-3">
        <span className="flex flex-col gap-1.5">
          <Eyebrow>Workspace</Eyebrow>
          <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
            Overview
          </h1>
          <span className="text-[13.5px] text-fg2">
            {total} {total === 1 ? "resource" : "resources"} in{" "}
            {namespacesRead(namespaces.length, scope.readable.length)} this identity may read.
          </span>
        </span>
        <Button onClick={() => void queryClient.invalidateQueries()} disabled={scope.isFetching}>
          {scope.isFetching ? "Reading…" : "Refresh"}
        </Button>
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
            <TileMovement state={state.value} moved={moved} now={now} />
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
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)]">
        <ExecutionsSection days={perDay(feed, kept.complete, now)} complete={kept.complete} />
        <LifecycleSection />
      </div>
      {kept.complete ? (
        <JournalActivitySection entries={feed} now={now} />
      ) : (
        <ActivitySection resources={resources} now={now} />
      )}
    </>
  )
}

/** What the journals say moved in the period the tiles count, for the states it bears on. */
function TileMovement({
  state,
  moved,
  now,
}: {
  state: ResourceState
  moved: Movement | undefined
  now: Date
}) {
  if (!moved) {
    return null
  }
  const counted: Partial<Record<ResourceState, [number, string, string]>> = {
    attention: [moved.failures, "failure", "failures"],
    progress: [moved.executions, "Apply or run", "Applies and runs"],
    insync: [moved.verified, "verified in sync", "verified in sync"],
  }
  const entry = counted[state]
  if (!entry) {
    return null
  }
  const [count, one, many] = entry
  const day = now.getTime() - moved.since.getTime() >= 86_399_000
  return (
    <span className="text-xs text-mute">
      {count} {count === 1 ? one : many}{" "}
      {day ? "in the last 24 h" : `since ${moved.since.toLocaleTimeString()}`}
    </span>
  )
}

const dayFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
})

/**
 * Plans published and Applies and runs that ended, per day, over the days the
 * journals cover; never a week the operator did not keep.
 */
function ExecutionsSection({ days, complete }: { days: DayCount[]; complete?: Date }) {
  if (!complete || days.length === 0) {
    return (
      <Section title="Plans and executions">
        <p className="m-0 text-sm text-fg2">
          No journal is kept for these resources, so there is nothing to count per day.
        </p>
      </Section>
    )
  }
  const most = Math.max(1, ...days.map((d) => d.plans + d.succeeded + d.failed + d.unknown))
  const todayFrom = days.at(-1)!.day.getTime() < complete.getTime() ? complete : undefined
  const parts: { key: keyof Omit<DayCount, "day">; label: string; color: string }[] = [
    { key: "succeeded", label: "Succeeded", color: "bg-green" },
    { key: "failed", label: "Failed", color: "bg-red" },
    { key: "unknown", label: "Outcome unknown", color: "bg-violet" },
    { key: "plans", label: "Plans published", color: "bg-line2" },
  ]
  return (
    <Section
      title="Plans and executions"
      sub={`Per UTC day, over the ${days.length} ${days.length === 1 ? "day" : "days"} the journals cover`}
    >
      <ul aria-label="Legend" className="m-0 flex list-none flex-wrap gap-3 p-0 text-xs text-fg2">
        {parts.map((part) => (
          <li key={part.key} className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className={`size-2.5 rounded-sm ${part.color}`} />
            {part.label}
          </li>
        ))}
      </ul>
      <div aria-hidden="true" className="flex h-[140px] items-end gap-2">
        {days.map((day) => (
          <span
            key={day.day.toISOString()}
            className="flex h-full flex-1 flex-col items-center gap-1"
          >
            <span className="flex w-full max-w-[42px] flex-auto flex-col-reverse overflow-hidden rounded-md bg-[color-mix(in_oklab,var(--line)_35%,transparent)]">
              {parts.map((part) =>
                day[part.key] > 0 ? (
                  <span
                    key={part.key}
                    className={part.color}
                    style={{ height: `${(day[part.key] / most) * 100}%` }}
                  />
                ) : null
              )}
            </span>
            <span className="text-[11px] text-mute">{dayFormat.format(day.day)}</span>
          </span>
        ))}
      </div>
      <table className="sr-only">
        <caption>Plans and executions per UTC day</caption>
        <thead>
          <tr>
            <th scope="col">Day</th>
            {parts.map((part) => (
              <th key={part.key} scope="col">
                {part.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {days.map((day) => (
            <tr key={day.day.toISOString()}>
              <th scope="row">{dayFormat.format(day.day)}</th>
              {parts.map((part) => (
                <td key={part.key}>{day[part.key]}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {todayFrom ? (
        <span className="text-xs text-mute">
          The journals are complete only since {todayFrom.toLocaleString()}, so today counts from
          then.
        </span>
      ) : null}
    </Section>
  )
}

const lifecycleSteps = [
  "Resolve the source and pin what it names",
  "Verify it under the verification policy",
  "Observe the database, read-only",
  "Plan the exact change",
  "Wait for an approval where the policy asks",
  "Apply the approved plan in a Job",
  "Verify convergence with a fresh observation",
]

/** The loop every resource repeats, beside the counts it produces. */
function LifecycleSection() {
  return (
    <Section title="Operator lifecycle" sub="What each resource repeats at its interval">
      <ol className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
        {lifecycleSteps.map((step, index) => (
          <li key={step} className="flex items-center gap-2.5">
            <span className="flex size-5 flex-none items-center justify-center rounded-full bg-pribg font-mono text-[11px] text-pri">
              {index + 1}
            </span>
            {step}
          </li>
        ))}
      </ol>
      <span className="text-xs text-mute">
        ↻ The loop repeats at each resource&apos;s interval, so drift and a moved tag are noticed
        without anyone asking.{" "}
        <Link to="/console/architecture" className="font-medium text-pri">
          How it works →
        </Link>
      </span>
    </Section>
  )
}

/** The newest events the journals and the decisions record, with the way to all of them. */
function JournalActivitySection({ entries, now }: { entries: ActivityEntry[]; now: Date }) {
  const shown = entries.slice(0, activityRows)
  return (
    <Section title="Recent activity" sub="The newest events the journals and the decisions record">
      {shown.length === 0 ? (
        <p className="m-0 text-sm text-fg2">No journal holds an event yet.</p>
      ) : (
        <ol className="m-0 flex list-none flex-col p-0">
          {shown.map((entry) => (
            <li
              key={entry.key}
              className="grid grid-cols-[22px_minmax(0,1fr)_auto] items-center gap-3 border-b border-line py-2 last:border-b-0"
            >
              <StatusGlyph tone={entry.tone} size={20} />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="truncate text-sm font-medium">{entry.title}</span>
                <span className="truncate text-xs text-mute">
                  {entry.namespace}/{entry.name} · {entry.kind}
                </span>
              </span>
              <time
                dateTime={entry.at.toISOString()}
                title={entry.at.toLocaleString()}
                className="font-mono text-xs whitespace-nowrap text-mute"
              >
                {ago(entry.at, now)}
              </time>
            </li>
          ))}
        </ol>
      )}
      <span className="flex text-xs">
        <span className="flex-auto" />
        <Link to="/console/activity" className="font-medium text-pri">
          All activity →
        </Link>
      </span>
    </Section>
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
        <ScrollRegion label="Resources that need attention">
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
        </ScrollRegion>
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
  const [query, setQuery] = useState("")
  const [expanded, setExpanded] = useState(false)
  const all = namespaceHealth(resources, scope)
  const wanted = query.trim().toLowerCase()
  const matched = all.filter((row) => row.namespace.toLowerCase().includes(wanted))
  const rows = expanded ? matched : matched.slice(0, namespaceRows)
  return (
    <Section title="Namespace health" sub="Resource states by namespace">
      {all.length > namespaceRows ? (
        <label className="flex h-9 items-center gap-2 rounded-[10px] border border-line bg-bg px-2.5 text-mute">
          <span aria-hidden="true">⌕</span>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={`Search ${all.length} namespaces…`}
            aria-label="Search namespaces"
            className="min-w-0 flex-auto border-0 bg-transparent text-sm text-fg outline-0"
          />
        </label>
      ) : null}
      <ScrollRegion label="Resource states by namespace">
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
                    <Count value={row.counts.progress} tone="text-run" />
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
      </ScrollRegion>
      {matched.length === 0 ? (
        <p className="m-0 text-sm text-fg2">No namespace matches “{query.trim()}”.</p>
      ) : null}
      {matched.length > namespaceRows ? (
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="self-start cursor-pointer border-0 bg-transparent p-0 text-xs font-medium text-pri"
        >
          {expanded ? "Show fewer" : `Show all ${matched.length}`}
        </button>
      ) : null}
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
