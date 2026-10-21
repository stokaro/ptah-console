import { timestampDate } from "@bufbuild/protobuf/wkt"

import {
  Access,
  AttentionKind,
  type Attention,
  type Condition,
  type NamespaceScope,
} from "../gen/ptah/console/v1/resources_pb"
import { stateOf, type ResourceState } from "./catalog"
import type { ResourceView } from "./resource"

/** The kinds of attention the overview groups, in the order a person should look. */
export type AttentionGroup = "unknown" | "failing" | "approval" | "drift" | "stale"

export const attentionGroups: { value: AttentionGroup; label: string }[] = [
  { value: "unknown", label: "Outcome unknown" },
  { value: "failing", label: "Failed or blocked" },
  { value: "approval", label: "Approval" },
  { value: "drift", label: "Drift" },
  { value: "stale", label: "Stale" },
]

const groupOf: Partial<Record<AttentionKind, AttentionGroup>> = {
  [AttentionKind.OUTCOME_UNKNOWN]: "unknown",
  [AttentionKind.UNRESOLVED_RUN]: "unknown",
  [AttentionKind.FAILED]: "failing",
  [AttentionKind.BLOCKED]: "failing",
  [AttentionKind.WAITING_FOR_APPROVAL]: "approval",
  [AttentionKind.DRIFT]: "drift",
  [AttentionKind.STALE_STATUS]: "stale",
}

/**
 * What brings a resource to the needs-attention table: its most important
 * attention, which the server lists first. A suspension is not one; a
 * suspended resource is filed as suspended.
 */
export function leadingAttention(resource: ResourceView): Attention | undefined {
  if (stateOf(resource) !== "attention") {
    return undefined
  }
  return resource.attention.find((a) => groupOf[a.kind] !== undefined)
}

export interface AttentionRow {
  resource: ResourceView
  attention: Attention
  group: AttentionGroup
}

const groupRank = Object.fromEntries(attentionGroups.map((g, index) => [g.value, index])) as Record<
  AttentionGroup,
  number
>

/**
 * The resources that need a person, most urgent group first and, within a
 * group, the one that has waited longest. An attention with no date sorts
 * after the dated ones of its group.
 */
export function needsAttention(resources: ResourceView[]) {
  const rows: AttentionRow[] = []
  for (const resource of resources) {
    const attention = leadingAttention(resource)
    const group = attention && groupOf[attention.kind]
    if (attention && group) {
      rows.push({ resource, attention, group })
    }
  }
  const since = (row: AttentionRow) =>
    row.attention.since ? timestampDate(row.attention.since).getTime() : Infinity
  return rows.sort(
    (a, b) =>
      groupRank[a.group] - groupRank[b.group] ||
      since(a) - since(b) ||
      a.resource.namespace.localeCompare(b.resource.namespace) ||
      a.resource.name.localeCompare(b.resource.name)
  )
}

export interface NamespaceHealth {
  namespace: string
  /** Whether the identity may read the namespace; counts are empty when it may not. */
  readable: boolean
  counts: Record<ResourceState, number>
  total: number
}

function emptyCounts(): Record<ResourceState, number> {
  return { attention: 0, progress: 0, insync: 0, suspended: 0, none: 0 }
}

/**
 * One row per namespace in scope, the readable ones with the most attention
 * first. A namespace the identity may not read keeps its row and says so,
 * so it is not mistaken for an empty one.
 */
export function namespaceHealth(resources: ResourceView[], scopes: NamespaceScope[]) {
  const rows = new Map<string, NamespaceHealth>()
  for (const scope of scopes) {
    rows.set(scope.name, {
      namespace: scope.name,
      readable: scope.access === Access.OK,
      counts: emptyCounts(),
      total: 0,
    })
  }
  for (const resource of resources) {
    let row = rows.get(resource.namespace)
    if (!row) {
      row = { namespace: resource.namespace, readable: true, counts: emptyCounts(), total: 0 }
      rows.set(resource.namespace, row)
    }
    row.counts[stateOf(resource)]++
    row.total++
  }
  return [...rows.values()].sort(
    (a, b) =>
      Number(b.readable) - Number(a.readable) ||
      b.counts.attention - a.counts.attention ||
      a.namespace.localeCompare(b.namespace)
  )
}

/** How many resources are in each state, and in all. */
export function stateCounts(resources: ResourceView[]) {
  const counts = emptyCounts()
  for (const resource of resources) {
    counts[stateOf(resource)]++
  }
  return { counts, total: resources.length }
}

export interface Activity {
  resource: ResourceView
  condition: Condition
  at: Date
}

/**
 * The newest condition transitions across the resources: what their status
 * still records, newest first. It is not a history: a condition keeps only
 * its last transition, and an undated one is left out.
 */
export function recentActivity(resources: ResourceView[], limit: number): Activity[] {
  const activity: Activity[] = []
  for (const resource of resources) {
    for (const condition of resource.conditions) {
      if (condition.lastTransition) {
        activity.push({ resource, condition, at: timestampDate(condition.lastTransition) })
      }
    }
  }
  return activity.sort((a, b) => b.at.getTime() - a.at.getTime()).slice(0, limit)
}
