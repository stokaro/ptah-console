import { Card, Segmented, StatusGlyph, type Tone } from "@ptah/console-ui"
import { useState } from "react"

import { coverage, duration, type ActivityEntry } from "../../model/activity"
import { ago } from "../../model/connection"
import type { ResourceView } from "../../model/resource"
import { filterRuns, migrationRuns, runCounts, timedRuns, type RunFilter } from "../../model/runs"
import { GlanceTiles, type GlanceTile } from "./glance"

const barColor: Partial<Record<Tone, string>> = {
  ok: "bg-green",
  bad: "bg-red",
  unk: "bg-violet",
}

/** The numbers a reader checks first: how far the history is, what waits, and how runs ended. */
export function MigrationGlance({ resource }: { resource: ResourceView }) {
  const history = resource.migration?.history
  const runs = migrationRuns(resource)
  const kept = coverage([resource])
  const failed = runCounts(runs).failed
  const tiles: GlanceTile[] = [
    {
      label: "Current version",
      value: history ? String(history.currentVersion) : "–",
      sub: history
        ? history.checkpointVersion
          ? `checkpoint ${history.checkpointVersion}`
          : "no checkpoint"
        : "the history is not read yet",
      tone: history ? (history.dirty ? "bad" : "ok") : "off",
    },
    {
      label: "Applied",
      value: history ? String(history.appliedCount) : "–",
      sub: "of the artifact's migrations",
      tone: "off",
    },
    {
      label: "Pending",
      value: history ? String(history.pendingCount) : "–",
      sub: history?.dirty ? "held: the revision table is dirty" : "waiting in the artifact",
      tone: history && history.pendingCount > 0 ? "warn" : "off",
    },
    {
      label: "Runs kept",
      value: kept.complete ? String(runs.length) : "–",
      sub: kept.complete
        ? `${failed} failed since ${kept.complete.toLocaleDateString()}`
        : "the operator keeps no journal",
      tone: failed > 0 ? "bad" : "off",
    },
  ]
  return <GlanceTiles tiles={tiles} />
}

/**
 * The runs the journal keeps, newest first, with how each ended and how long
 * it took, and a chart of the durations. The operator records a run, not each
 * migration in it, so the chart is per run.
 */
export function RunsCard({ resource, now }: { resource: ResourceView; now: Date }) {
  const [filter, setFilter] = useState<RunFilter>("all")
  const runs = migrationRuns(resource)
  const kept = coverage([resource])
  if (!kept.complete) {
    return (
      <Card title="Runs">
        <span className="text-sm text-fg2">
          The operator keeps no journal of this migration, so its runs before the last one are not
          known.
        </span>
      </Card>
    )
  }
  const counts = runCounts(runs)
  const shown = filterRuns(runs, filter)
  return (
    <Card title="Runs">
      <Segmented
        label="Show runs"
        value={filter}
        onChange={setFilter}
        options={[
          { value: "all", label: "All", count: counts.all },
          { value: "succeeded", label: "Succeeded", count: counts.succeeded },
          { value: "failed", label: "Failed", count: counts.failed },
          { value: "unknown", label: "Outcome unknown", count: counts.unknown },
        ]}
      />
      {shown.length === 0 ? (
        <span className="text-sm text-fg2">
          {runs.length === 0 ? "No run ended since the journal began." : "No run matches."}
        </span>
      ) : (
        <ol aria-label="Runs, newest first" className="m-0 flex list-none flex-col p-0">
          {shown.map((run) => (
            <li
              key={run.key}
              className="grid grid-cols-[20px_minmax(0,1fr)_auto] gap-2.5 border-b border-line py-2 text-[13px] last:border-b-0"
            >
              <StatusGlyph tone={run.tone} size={20} />
              <span className="flex min-w-0 flex-col">
                <b className="font-semibold">{run.title}</b>
                <span className="text-xs text-fg2">{run.details.join(" · ")}</span>
              </span>
              <span className="flex flex-col items-end text-xs whitespace-nowrap text-mute">
                <span>{ago(run.at, now)}</span>
                {run.durationMs !== undefined ? <span>took {duration(run.durationMs)}</span> : null}
              </span>
            </li>
          ))}
        </ol>
      )}
      <RunDurations runs={runs} />
      <span className="text-xs text-mute">
        From the operator&apos;s journal of this migration, complete since{" "}
        {kept.complete.toLocaleString()}.
      </span>
    </Card>
  )
}

function RunDurations({ runs }: { runs: ActivityEntry[] }) {
  const timed = timedRuns(runs)
  if (timed.length === 0) {
    return null
  }
  const longest = Math.max(1, ...timed.map((run) => run.durationMs))
  return (
    <figure className="m-0 flex flex-col gap-1.5">
      <figcaption className="text-xs font-medium text-fg2">
        How long each run took, oldest first
      </figcaption>
      <div aria-hidden="true" className="flex h-[96px] items-end gap-1">
        {timed.map((run) => (
          <span
            key={run.key}
            title={`${run.title}: ${duration(run.durationMs)}`}
            className={`max-w-[28px] min-w-[3px] flex-1 rounded-t-[4px] ${barColor[run.tone] ?? "bg-line2"}`}
            style={{ height: `${Math.max(4, (run.durationMs / longest) * 100)}%` }}
          />
        ))}
      </div>
      <table className="sr-only">
        <caption>How long each run took</caption>
        <thead>
          <tr>
            <th scope="col">Ended</th>
            <th scope="col">Run</th>
            <th scope="col">Took</th>
          </tr>
        </thead>
        <tbody>
          {timed.map((run) => (
            <tr key={run.key}>
              <th scope="row">{run.at.toLocaleString()}</th>
              <td>{run.title}</td>
              <td>{duration(run.durationMs)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  )
}
