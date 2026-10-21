import { timestampDate } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import { AttentionKind, StageState } from "../gen/ptah/console/v1/resources_pb"
import { attentionTitle, attentionTone, type ResourceKind, type ResourceView } from "./resource"

/** Where a resource stands, in the few states a list filters by. */
export type ResourceState = "attention" | "progress" | "insync" | "suspended" | "none"

export const resourceStates: { value: ResourceState; label: string }[] = [
  { value: "attention", label: "Needs attention" },
  { value: "progress", label: "In progress" },
  { value: "insync", label: "In sync" },
  { value: "suspended", label: "Suspended" },
  { value: "none", label: "No status" },
]

/**
 * The state a list files a resource under. A suspended resource is suspended
 * whatever else it waits on, because nothing moves until someone resumes it;
 * anything else a person has to look at comes next, then work under way, then
 * a resource whose every stage is done.
 */
export function stateOf(resource: ResourceView): ResourceState {
  if (resource.suspended || resource.attention.some((a) => a.kind === AttentionKind.SUSPENDED)) {
    return "suspended"
  }
  if (resource.attention.length > 0) {
    return "attention"
  }
  if (resource.stages.some((s) => s.state === StageState.ACTIVE)) {
    return "progress"
  }
  const done =
    resource.stages.length > 0 &&
    resource.stages.every(
      (s) => s.state === StageState.DONE || s.state === StageState.NOT_APPLICABLE
    )
  return done ? "insync" : "none"
}

/** The state as a list shows it: what kind of attention, not only that there is some. */
export function stateLabel(resource: ResourceView): { tone: Tone; text: string } {
  const state = stateOf(resource)
  switch (state) {
    case "attention": {
      const first = resource.attention.find((a) => a.kind !== AttentionKind.SUSPENDED)!
      return { tone: attentionTone(first.kind), text: attentionTitle(first.kind) }
    }
    case "progress":
      return { tone: "run", text: "In progress" }
    case "insync":
      return { tone: "ok", text: "In sync" }
    case "suspended":
      return { tone: "off", text: "Suspended" }
    default:
      return { tone: "off", text: resource.phase || "No status" }
  }
}

/** When the status last moved: the newest condition transition. */
export function updatedAt(resource: ResourceView): Date | undefined {
  let newest: Date | undefined
  for (const condition of resource.conditions) {
    if (!condition.lastTransition) {
      continue
    }
    const at = timestampDate(condition.lastTransition)
    if (!newest || at > newest) {
      newest = at
    }
  }
  return newest
}

export type KindFilter = "all" | ResourceKind
export type StateFilter = "all" | ResourceState
export type SortOrder = "attention" | "name" | "namespace" | "updated"

export const sortOrders: { value: SortOrder; label: string }[] = [
  { value: "attention", label: "Attention first" },
  { value: "name", label: "Name" },
  { value: "namespace", label: "Namespace" },
  { value: "updated", label: "Recently updated" },
]

export interface CatalogQuery {
  q: string
  kind: KindFilter
  state: StateFilter
  sort: SortOrder
}

/** Whether the search text names the resource, its namespace, why it waits, or its artifact. */
export function matchesText(resource: ResourceView, text: string) {
  const q = text.trim().toLowerCase()
  if (q === "") {
    return true
  }
  return [
    resource.name,
    resource.namespace,
    resource.source,
    resource.sourceDigest,
    ...resource.attention.map((a) => a.reason),
  ].some((field) => field.toLowerCase().includes(q))
}

const stateRank: Record<ResourceState, number> = {
  attention: 0,
  progress: 1,
  none: 2,
  suspended: 3,
  insync: 4,
}

function byName(a: ResourceView, b: ResourceView) {
  return a.name.localeCompare(b.name) || a.namespace.localeCompare(b.namespace)
}

function byNamespace(a: ResourceView, b: ResourceView) {
  return a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name)
}

const sorts: Record<SortOrder, (a: ResourceView, b: ResourceView) => number> = {
  attention: (a, b) => stateRank[stateOf(a)] - stateRank[stateOf(b)] || byNamespace(a, b),
  name: byName,
  namespace: byNamespace,
  // A resource whose status never moved goes last, not first.
  updated: (a, b) =>
    (updatedAt(b)?.getTime() ?? -Infinity) - (updatedAt(a)?.getTime() ?? -Infinity) ||
    byNamespace(a, b),
}

/**
 * The resources a query selects, in its order. The counts beside each filter
 * are taken with the other filters applied, so a number says what clicking it
 * would show.
 */
export function select(resources: ResourceView[], query: CatalogQuery) {
  const texted = resources.filter((r) => matchesText(r, query.q))
  const kindCounts: Record<KindFilter, number> = { all: 0, schema: 0, migration: 0 }
  const stateCounts: Record<StateFilter, number> = {
    all: 0,
    attention: 0,
    progress: 0,
    insync: 0,
    suspended: 0,
    none: 0,
  }
  for (const resource of texted) {
    const state = stateOf(resource)
    if (query.state === "all" || query.state === state) {
      kindCounts.all++
      kindCounts[resource.kind]++
    }
    if (query.kind === "all" || query.kind === resource.kind) {
      stateCounts.all++
      stateCounts[state]++
    }
  }
  const rows = texted
    .filter((r) => query.kind === "all" || r.kind === query.kind)
    .filter((r) => query.state === "all" || stateOf(r) === query.state)
    .sort(sorts[query.sort])
  return { rows, kindCounts, stateCounts }
}

export const pageSizes = [25, 50, 100] as const

/** One page of rows, with the page clamped into range when the rows got fewer. */
export function paginate<T>(rows: T[], page: number, size: number) {
  const pages = Math.max(1, Math.ceil(rows.length / size))
  const current = Math.min(Math.max(1, Math.floor(page)), pages)
  const start = (current - 1) * size
  return {
    page: current,
    pages,
    rows: rows.slice(start, start + size),
    first: rows.length === 0 ? 0 : start + 1,
    last: Math.min(start + size, rows.length),
    total: rows.length,
  }
}
