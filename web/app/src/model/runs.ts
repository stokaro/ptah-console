import type { Tone } from "@ptah/console-ui"

import { activity, type ActivityEntry } from "./activity"
import type { ResourceView } from "./resource"

/** Which runs the list shows: every run, or those that ended one way. */
export type RunFilter = "all" | "succeeded" | "failed" | "unknown"

const filterTones: Record<Exclude<RunFilter, "all">, Tone> = {
  succeeded: "ok",
  failed: "bad",
  unknown: "unk",
}

/** The runs a migration's journal keeps, newest first. */
export function migrationRuns(resource: ResourceView): ActivityEntry[] {
  if (!resource.migration) {
    return []
  }
  return activity([resource], []).filter((entry) => entry.type === "apply")
}

export function filterRuns(runs: ActivityEntry[], filter: RunFilter): ActivityEntry[] {
  return filter === "all" ? runs : runs.filter((run) => run.tone === filterTones[filter])
}

export function runCounts(runs: ActivityEntry[]): Record<RunFilter, number> {
  return {
    all: runs.length,
    succeeded: filterRuns(runs, "succeeded").length,
    failed: filterRuns(runs, "failed").length,
    unknown: filterRuns(runs, "unknown").length,
  }
}

/**
 * The runs that record how long they took, oldest first, as a chart reads
 * them. A run the operator could not settle records no start, and is left
 * out rather than drawn as instant.
 */
export function timedRuns(runs: ActivityEntry[]): (ActivityEntry & { durationMs: number })[] {
  return runs
    .filter((run): run is ActivityEntry & { durationMs: number } => (run.durationMs ?? -1) >= 0)
    .reverse()
}
