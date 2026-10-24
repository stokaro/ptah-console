import { timestampDate } from "@bufbuild/protobuf/wkt"

import type { ResourceView } from "./resource"

/**
 * How far past its next read a resource may be before it counts as late. The
 * operator paces its requests and works through a queue, so a read is seldom
 * on the second, and a minute late says nothing about whether it runs.
 */
export const lateAfterMs = 5 * 60_000

/** What the readable resources say about whether the operator keeps its schedule. */
export interface Punctuality {
  /** How many resources promised a read the console can hold the operator to. */
  judged: number
  /** The ones past that read by more than lateAfterMs, the latest first. */
  late: ResourceView[]
}

/**
 * Holds the operator to the next read each resource's status promises. A
 * suspended resource promises none, and one with an operation in flight or a
 * proof pending reads when that work ends rather than at the promised time,
 * so neither is judged. The console does not ask the cluster which operator
 * runs; a schedule the resources stopped keeping is the evidence it has.
 */
export function punctuality(resources: ResourceView[], now: Date): Punctuality {
  const late: { resource: ResourceView; by: number }[] = []
  let judged = 0
  for (const resource of resources) {
    const next = nextReadOf(resource)
    if (!next || resource.suspended || busy(resource)) {
      continue
    }
    judged++
    const by = now.getTime() - next.getTime()
    if (by > lateAfterMs) {
      late.push({ resource, by })
    }
  }
  late.sort((a, b) => b.by - a.by)
  return { judged, late: late.map((entry) => entry.resource) }
}

function nextReadOf(resource: ResourceView) {
  const next = resource.schema?.nextReconciliation ?? resource.migration?.nextReconciliation
  return next ? timestampDate(next) : undefined
}

function busy(resource: ResourceView) {
  if (resource.schema) {
    return Boolean(resource.schema.activeOperation || resource.schema.pendingObservation)
  }
  return Boolean(resource.migration?.activeOperation)
}
