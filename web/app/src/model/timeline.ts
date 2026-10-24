import { timestampDate } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import type {
  Migration,
  MigrationPlan,
  MigrationRevision,
} from "../gen/ptah/console/v1/resources_pb"
import { duration, whereStopped } from "./activity"

/** Where one migration of the sequence stands. */
export type StepState =
  "applied" | "modified" | "dirty" | "failed" | "pending" | "held" | "out-of-order"

/** One migration of the sequence, as the page can account for it. */
export interface TimelineStep {
  version: bigint
  /** The version as the files spell it, padded where they pad it. */
  label: string
  /** The migration's own description, from its file name. */
  name: string
  state: StepState
  tone: Tone
  /** When its revision row was written. */
  appliedAt?: Date
  /** How long it ran, where the revision table records it. */
  durationMs?: number
  /** One line on what the page knows of it beyond its state. */
  note: string
}

const stepTones: Record<StepState, Tone> = {
  applied: "ok",
  modified: "bad",
  dirty: "bad",
  failed: "bad",
  pending: "warn",
  held: "off",
  "out-of-order": "warn",
}

/** The states a filter groups, in the order the page offers them. */
export type StepFilter = "all" | "applied" | "pending" | "failed"

const filterStates: Record<Exclude<StepFilter, "all">, StepState[]> = {
  applied: ["applied"],
  pending: ["pending", "held", "out-of-order"],
  failed: ["failed", "dirty", "modified"],
}

/** Whether a step needs a person: what "Jump to failed" lands on. */
export function needsAPerson(step: TimelineStep) {
  return filterStates.failed.includes(step.state)
}

/** How long a migration ran, in the unit that says it: milliseconds below a second. */
export function runTime(ms: number) {
  if (ms < 1000) {
    return `${ms} ms`
  }
  if (ms < 60_000) {
    return `${(Math.round(ms / 100) / 10).toFixed(1)} s`
  }
  return duration(ms)
}

function revisionStep(row: MigrationRevision): TimelineStep {
  const state: StepState =
    row.state === "modified" ? "modified" : row.state === "dirty" ? "dirty" : "applied"
  const appliedAt = row.appliedAt ? timestampDate(row.appliedAt) : undefined
  const durationMs = row.executionMillis === undefined ? undefined : Number(row.executionMillis)
  const notes = {
    applied: "Applied",
    modified: "Applied, and its file no longer accounts for it",
    dirty: "A failed or interrupted run left this row",
  }
  return {
    version: row.version,
    label: String(row.version),
    name: row.description,
    state,
    tone: stepTones[state],
    appliedAt,
    durationMs,
    note: notes[state as keyof typeof notes],
  }
}

/**
 * The migration's sequence, newest first: the revision rows the operator
 * published, the migration the last run stopped in, the files the current
 * plan would apply and the versions the history refuses as out of order. A
 * version is listed once, by what the database says of it first.
 */
export function migrationTimeline(migration: Migration, plan?: MigrationPlan): TimelineStep[] {
  const steps = new Map<bigint, TimelineStep>()
  const add = (step: TimelineStep) => {
    if (!steps.has(step.version)) {
      steps.set(step.version, step)
    }
  }
  for (const row of migration.history?.revisions ?? []) {
    add(revisionStep(row))
  }
  const run = migration.lastRun
  if (run?.failure && (run.outcome === "Failed" || run.outcome === "Partial")) {
    add({
      version: run.failure.version,
      label: String(run.failure.version),
      name: "",
      state: "failed",
      tone: stepTones.failed,
      note: `The last run stopped here: ${whereStopped(run.failure)}`,
    })
  }
  const dirty = migration.history?.dirty ?? false
  for (const file of plan?.migrations ?? []) {
    const state: StepState = dirty ? "held" : "pending"
    add({
      version: file.version,
      label: file.versionKey || String(file.version),
      name: file.description,
      state,
      tone: stepTones[state],
      note: dirty
        ? "In the plan, held: nothing applies while the history is dirty"
        : "In the plan, waiting to apply",
    })
  }
  for (const version of migration.history?.outOfOrderVersions ?? []) {
    add({
      version,
      label: String(version),
      name: "",
      state: "out-of-order",
      tone: stepTones["out-of-order"],
      note: "Below a version the database already applied; linear execution refuses it",
    })
  }
  return [...steps.values()].sort((a, b) =>
    a.version === b.version ? 0 : a.version > b.version ? -1 : 1
  )
}

/** The steps a filter and a search leave, in their order. */
export function filterSteps(steps: TimelineStep[], filter: StepFilter, query: string) {
  const wanted = query.trim().toLowerCase()
  return steps.filter(
    (step) =>
      (filter === "all" || filterStates[filter].includes(step.state)) &&
      (wanted === "" ||
        step.label.toLowerCase().includes(wanted) ||
        step.name.toLowerCase().includes(wanted))
  )
}

export function stepCounts(steps: TimelineStep[]): Record<StepFilter, number> {
  return {
    all: steps.length,
    applied: filterSteps(steps, "applied", "").length,
    pending: filterSteps(steps, "pending", "").length,
    failed: filterSteps(steps, "failed", "").length,
  }
}

/**
 * How many applied migrations the list leaves out: the history counts them,
 * and the operator publishes only its newest rows.
 */
export function unlistedApplied(migration: Migration) {
  const history = migration.history
  if (!history) {
    return 0
  }
  const listed = history.revisions.filter((row) => row.state === "applied").length
  return Math.max(0, history.appliedCount - listed)
}
