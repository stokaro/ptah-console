import { EmptyState, Eyebrow, Segmented, StatusGlyph } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { useDecisions, useResources } from "../../api/resources"
import {
  activity,
  activityPeriods,
  activityTypes,
  coverage,
  deciders,
  filterActivity,
  type ActivityEntry,
  type ActivityFilter,
  type Coverage,
} from "../../model/activity"
import { ago } from "../../model/connection"
import { Loading, ReadError, useNow } from "../common"

export interface ActivityProps {
  search: ActivityFilter
  onSearch: (search: ActivityFilter) => void
}

/** How long an Apply or a run took, in words. */
function duration(ms: number) {
  if (ms < 60_000) {
    return `${Math.max(1, Math.round(ms / 1000))} s`
  }
  const minutes = Math.round(ms / 60_000)
  return minutes < 120 ? `${minutes} min` : `${Math.round(minutes / 60)} h`
}

/**
 * What happened across the namespaces the console reads: plans, approvals,
 * Applies and runs, verifications, acknowledgments and failures, from the
 * journal each resource keeps and the decisions people made. Only what the
 * operator kept is shown, and the page says from when that is complete.
 */
export function Activity({ search, onSearch }: ActivityProps) {
  const { resources, loading, error } = useResources()
  const { decisions } = useDecisions()
  const now = useNow()
  // A feed that changed once the decisions arrived would move under the
  // reader, so the page waits for them; one the API server refuses is said.
  if (loading || (decisions.isPending && decisions.fetchStatus !== "idle")) {
    return <Loading what="the activity" />
  }
  if (error) {
    return <ReadError error={error} what="the activity" />
  }
  const all = activity(resources, decisions.data?.decisions ?? [])
  const shown = filterActivity(all, search, now)
  const kept = coverage(resources)
  const people = deciders(all)
  const filtered = Boolean(search.type || search.period || search.who)
  const change = (next: ActivityFilter) => onSearch({ ...search, ...next })

  return (
    <>
      <section className="flex flex-col gap-1.5">
        <Eyebrow>Installation</Eyebrow>
        <h1 className="m-0 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Activity
        </h1>
        <span className="max-w-[760px] text-[13.5px] text-fg2">
          Plans, approvals, Applies and runs, verifications and failures across the namespaces the
          console reads. Only what the operator kept is here: each resource keeps its latest 64
          events, none more than 30 days older than its newest.
        </span>
        <CoverageLine coverage={kept} now={now} />
        {decisions.error ? (
          <span role="status" className="text-xs text-amber">
            Approvals and acknowledgments are not listed: {decisions.error.message}
          </span>
        ) : null}
      </section>

      <section aria-label="Filters" className="flex flex-col gap-2.5">
        <div className="flex flex-wrap items-center gap-2.5">
          <span className="text-xs text-mute">Type</span>
          <Segmented
            label="Type"
            value={search.type ?? "all"}
            onChange={(type) => change({ type: type === "all" ? undefined : type })}
            options={[
              { value: "all", label: "All", count: all.length },
              ...activityTypes.map((type) => ({
                value: type.value,
                label: type.label,
                count: all.filter((entry) => entry.type === type.value).length,
              })),
            ]}
          />
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          <span className="text-xs text-mute">Period</span>
          <Segmented
            label="Period"
            value={search.period ?? "all"}
            onChange={(period) => change({ period: period === "all" ? undefined : period })}
            options={activityPeriods.map((period) => ({
              value: period.value,
              label: period.label,
            }))}
          />
          {people.length > 0 ? (
            <>
              <span className="text-xs text-mute">Who</span>
              <Segmented
                label="Who"
                value={search.who ?? ""}
                onChange={(who) => change({ who: who || undefined })}
                options={[
                  { value: "", label: "Everyone" },
                  ...people.map((person) => ({ value: person, label: person })),
                ]}
              />
            </>
          ) : null}
        </div>
      </section>

      {shown.length === 0 ? (
        <EmptyState
          tone="off"
          title={filtered ? "No entries match these filters" : "Nothing kept yet"}
        >
          {filtered
            ? "Try another type or a longer period."
            : "No journal the console reads holds an event yet, and nobody has decided anything here."}
        </EmptyState>
      ) : (
        <section aria-label="Entries" className="flex flex-col gap-2">
          <span role="status" className="text-xs text-mute">
            {shown.length} {shown.length === 1 ? "entry" : "entries"}
          </span>
          <ol className="m-0 flex list-none flex-col gap-2 p-0">
            {shown.map((entry) => (
              <Entry key={entry.key} entry={entry} now={now} />
            ))}
          </ol>
        </section>
      )}
    </>
  )
}

function CoverageLine({ coverage: kept, now }: { coverage: Coverage; now: Date }) {
  if (!kept.complete) {
    return kept.withoutJournal > 0 ? (
      <span className="text-xs text-mute">
        The operator these resources run under keeps no journal, so only decisions are listed.
      </span>
    ) : null
  }
  const reachesFurther = kept.reaches && kept.reaches.getTime() < kept.complete.getTime()
  return (
    <span className="text-xs text-mute">
      Complete since {kept.complete.toLocaleString()} ({ago(kept.complete, now)}) for every resource
      read
      {reachesFurther && kept.reaches
        ? `; some journals reach back to ${kept.reaches.toLocaleString()}`
        : ""}
      .
      {kept.withoutJournal > 0
        ? ` ${kept.withoutJournal} ${kept.withoutJournal === 1 ? "resource runs" : "resources run"} under an operator that keeps no journal.`
        : ""}
    </span>
  )
}

function Entry({ entry, now }: { entry: ActivityEntry; now: Date }) {
  const page =
    entry.kind === "schema"
      ? ("/console/schemas/$namespace/$name" as const)
      : ("/console/migrations/$namespace/$name" as const)
  return (
    <li className="rounded-[14px] border border-line bg-card shadow-card">
      <details className="group">
        <summary className="grid cursor-pointer list-none grid-cols-[22px_minmax(0,1fr)_auto] items-center gap-3 px-4 py-3">
          <StatusGlyph tone={entry.tone} size={22} />
          <span className="flex min-w-0 flex-col">
            <b className="truncate text-[13.5px] font-semibold">{entry.title}</b>
            <span className="truncate text-xs text-mute">
              {entry.namespace}/{entry.name} · {entry.kind}
              {entry.durationMs !== undefined ? ` · took ${duration(entry.durationMs)}` : ""}
            </span>
          </span>
          <time
            dateTime={entry.at.toISOString()}
            title={entry.at.toLocaleString()}
            className="text-xs whitespace-nowrap text-mute"
          >
            {ago(entry.at, now)}
          </time>
        </summary>
        <div className="flex flex-col gap-1.5 border-t border-line px-4 py-3 text-[13px]">
          {entry.details.map((line) => (
            <span key={line} className="text-fg2">
              {line}
            </span>
          ))}
          <span className="text-xs text-mute">{entry.at.toLocaleString()}</span>
          <Link
            to={page}
            params={{ namespace: entry.namespace, name: entry.name }}
            className="text-xs font-medium text-pri"
          >
            Open {entry.namespace}/{entry.name}
          </Link>
        </div>
      </details>
    </li>
  )
}
