import { timestampDate } from "@bufbuild/protobuf/wkt"
import { Button, Card, ScrollRegion, Segmented, StatusGlyph, type Tone } from "@ptah/console-ui"
import { useEffect, useState } from "react"

import type { MigrationRevision } from "../../gen/ptah/console/v1/resources_pb"
import { ago } from "../../model/connection"
import {
  filterSteps,
  needsAPerson,
  runTime,
  stepCounts,
  type StepFilter,
  type TimelineStep,
} from "../../model/timeline"

/** How many steps the timeline shows at first, and adds each time. */
export const timelinePage = 50

const stepWords: Record<TimelineStep["state"], string> = {
  applied: "applied",
  modified: "modified",
  dirty: "dirty",
  failed: "failed",
  pending: "pending",
  held: "held",
  "out-of-order": "out of order",
}

const barColor: Partial<Record<Tone, string>> = {
  ok: "bg-green",
  bad: "bg-red",
  warn: "bg-amber",
}

/**
 * The migration's sequence, newest first, with a search, a filter by state,
 * a way to the step that needs a person, and the older steps a page at a
 * time.
 */
export function TimelineCard({
  name,
  steps,
  unlisted,
  now,
}: {
  name: string
  steps: TimelineStep[]
  /** Applied migrations the history counts and the operator lists no row for. */
  unlisted: number
  now: Date
}) {
  const [query, setQuery] = useState("")
  const [filter, setFilter] = useState<StepFilter>("all")
  const [limit, setLimit] = useState(timelinePage)
  const [jumped, setJumped] = useState<bigint>()
  const counts = stepCounts(steps)
  const matching = filterSteps(steps, filter, query)
  const shown = matching.slice(0, limit)
  const failed = steps.findIndex(needsAPerson)
  useEffect(() => {
    if (jumped !== undefined) {
      document.getElementById(`step-${jumped}`)?.focus()
    }
  }, [jumped])

  const jump = () => {
    setQuery("")
    setFilter("all")
    setLimit((current) => Math.max(current, failed + 1))
    setJumped(steps[failed].version)
  }
  return (
    <Card title="Migration timeline">
      <div className="flex flex-wrap items-center gap-2">
        <label className="flex h-9 min-w-[220px] flex-auto items-center gap-2 rounded-[10px] border border-line bg-bg px-2.5 text-mute">
          <span aria-hidden="true">⌕</span>
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Version or file name…"
            aria-label="Search the migrations"
            className="min-w-0 flex-auto border-0 bg-transparent text-sm text-fg outline-0"
          />
        </label>
        <Segmented
          label="Show migrations"
          value={filter}
          onChange={setFilter}
          options={(
            [
              { value: "all", label: "All" },
              { value: "applied", label: "Applied" },
              { value: "pending", label: "Pending" },
              { value: "failed", label: "Needs a person" },
            ] as const
          )
            .filter((option) => option.value === "all" || counts[option.value] > 0)
            .map((option) => ({ ...option, count: counts[option.value] }))}
        />
        <Button onClick={jump} disabled={failed < 0}>
          ↓ Jump to failed
        </Button>
      </div>
      {steps.length === 0 ? (
        <span className="text-sm text-fg2">
          The operator lists no migration of this history yet; it does after its next history read.
        </span>
      ) : shown.length === 0 ? (
        <span className="text-sm text-fg2">No migration matches.</span>
      ) : (
        <ol
          aria-label={`Migrations of ${name}, newest first`}
          className="m-0 flex list-none flex-col p-0"
        >
          {shown.map((step) => (
            <li
              key={String(step.version)}
              id={`step-${step.version}`}
              tabIndex={-1}
              className="grid grid-cols-[20px_minmax(64px,auto)_minmax(0,1fr)_auto] items-start gap-2.5 rounded-md border-b border-line py-2 text-[13px] last:border-b-0 focus-visible:outline-2 focus-visible:outline-pri"
            >
              <StatusGlyph tone={step.tone} size={20} />
              <span className="font-mono font-semibold">{step.label}</span>
              <span className="flex min-w-0 flex-col">
                <span className="truncate">
                  {step.name || <span className="text-mute">–</span>}
                </span>
                <span className="text-xs text-fg2">
                  {step.note}
                  {step.appliedAt ? `, ${ago(step.appliedAt, now)}` : ""}
                  {step.durationMs === undefined ? "" : `, ran ${runTime(step.durationMs)}`}
                </span>
              </span>
              <span className="text-xs whitespace-nowrap text-fg2">{stepWords[step.state]}</span>
            </li>
          ))}
        </ol>
      )}
      {matching.length > shown.length ? (
        <Button className="self-start" onClick={() => setLimit(limit + timelinePage)}>
          Show {Math.min(timelinePage, matching.length - shown.length)} more of{" "}
          {matching.length - shown.length} earlier
        </Button>
      ) : null}
      {unlisted > 0 ? (
        <span className="text-xs text-mute">
          The history counts {unlisted} more applied{" "}
          {unlisted === 1 ? "migration, which has" : "migrations, which have"} no row here: rows
          past the newest 64 the operator publishes, or versions a checkpoint covers.
        </span>
      ) : null}
    </Card>
  )
}

type Range = "10" | "25" | "all"

/**
 * How long each migration ran, oldest first, from the revision rows that
 * record it. A row without a duration is left out rather than drawn as
 * instant.
 */
export function ExecutionTimesCard({ revisions }: { revisions: MigrationRevision[] }) {
  const [range, setRange] = useState<Range>("25")
  const timed = revisions
    .filter((row) => row.executionMillis !== undefined)
    .map((row) => ({ row, ms: Number(row.executionMillis) }))
    .reverse()
  if (timed.length === 0) {
    return (
      <Card title="Migration execution times">
        <span className="text-sm text-fg2">
          No revision row records how long its migration ran. An executor before Ptah v0.16.0
          reports no duration, and a revision table too old to record one has none.
        </span>
      </Card>
    )
  }
  const shown = range === "all" ? timed : timed.slice(-Number(range))
  const longest = Math.max(1, ...shown.map((entry) => entry.ms))
  return (
    <Card title="Migration execution times">
      <Segmented
        label="Show versions"
        value={range}
        onChange={setRange}
        options={[
          ...(timed.length > 10 ? [{ value: "10" as const, label: "Last 10" }] : []),
          ...(timed.length > 25 ? [{ value: "25" as const, label: "Last 25" }] : []),
          { value: "all" as const, label: "All", count: timed.length },
        ]}
      />
      <figure className="m-0 flex flex-col gap-1.5">
        <figcaption className="text-xs font-medium text-fg2">
          How long each migration ran, oldest first
        </figcaption>
        <div aria-hidden="true" className="flex h-[112px] items-end gap-1">
          {shown.map(({ row, ms }) => (
            <span
              key={String(row.version)}
              title={`Version ${row.version}${row.description ? ` (${row.description})` : ""}: ${runTime(ms)}`}
              className={`max-w-[28px] min-w-[3px] flex-1 rounded-t-[4px] ${barColor[row.state === "applied" ? "ok" : "bad"] ?? "bg-line2"}`}
              style={{ height: `${Math.max(4, (ms / longest) * 100)}%` }}
            />
          ))}
        </div>
        <table className="sr-only">
          <caption>How long each migration ran</caption>
          <thead>
            <tr>
              <th scope="col">Version</th>
              <th scope="col">Migration</th>
              <th scope="col">Ran</th>
            </tr>
          </thead>
          <tbody>
            {shown.map(({ row, ms }) => (
              <tr key={String(row.version)}>
                <th scope="row">{String(row.version)}</th>
                <td>{row.description}</td>
                <td>{runTime(ms)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </figure>
    </Card>
  )
}

type RowFilter = "all" | MigrationRevision["state"]

/**
 * The revision table as the operator last read it: its newest rows, each
 * with the checksum it recorded, when it was written and how long it ran.
 */
export function RevisionHistoryCard({
  name,
  revisions,
  now,
}: {
  name: string
  revisions: MigrationRevision[]
  now: Date
}) {
  const [filter, setFilter] = useState<RowFilter>("all")
  const count = (state: string) => revisions.filter((row) => row.state === state).length
  const shown = revisions.filter((row) => filter === "all" || row.state === filter)
  return (
    <Card title="Migration history">
      {revisions.length === 0 ? (
        <span className="text-sm text-fg2">
          The operator lists no revision row yet; it does after its next history read.
        </span>
      ) : (
        <>
          <Segmented
            label="Show rows"
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All", count: revisions.length },
              ...["applied", "modified", "dirty"]
                .filter((state) => count(state) > 0)
                .map((state) => ({ value: state, label: state, count: count(state) })),
            ]}
          />
          <ScrollRegion label={`Revision rows of ${name}`}>
            <table className="w-full border-collapse text-left text-[13px]">
              <caption className="sr-only">Revision rows of {name}</caption>
              <thead>
                <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                  {["#", "Migration", "Checksum", "Status", "Applied", "Duration"].map((column) => (
                    <th key={column} scope="col" className="px-2 py-2 font-medium first:pl-0">
                      {column}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {shown.map((row) => {
                  const written = row.appliedAt ? timestampDate(row.appliedAt) : undefined
                  return (
                    <tr key={String(row.version)} className="border-b border-line last:border-b-0">
                      <th scope="row" className="py-2 pr-2 text-left font-mono font-medium">
                        {String(row.version)}
                      </th>
                      <td className="px-2 py-2">{row.description || "–"}</td>
                      <td className="px-2 py-2 font-mono text-xs" title={row.checksum}>
                        {row.checksum ? shortChecksum(row.checksum) : "–"}
                      </td>
                      <td className="px-2 py-2">
                        <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                          <StatusGlyph tone={row.state === "applied" ? "ok" : "bad"} size={20} />
                          {row.state}
                        </span>
                      </td>
                      <td className="px-2 py-2 whitespace-nowrap" title={written?.toLocaleString()}>
                        {written ? ago(written, now) : "–"}
                      </td>
                      <td className="py-2 pl-2 font-mono text-xs whitespace-nowrap">
                        {row.executionMillis === undefined
                          ? "–"
                          : runTime(Number(row.executionMillis))}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </ScrollRegion>
          <span className="text-xs text-mute">
            The newest rows the operator read, at most 64. A modified row shows the checksum the
            database recorded, not the file&apos;s.
          </span>
        </>
      )}
    </Card>
  )
}

/** A checksum's algorithm and its first and last characters. */
function shortChecksum(checksum: string) {
  const [algorithm, value] = checksum.includes(":") ? checksum.split(":", 2) : ["", checksum]
  const short = value.length > 14 ? `${value.slice(0, 6)}…${value.slice(-6)}` : value
  return algorithm ? `${algorithm}:${short}` : short
}
