import type { ResourceView } from "./resource"

/** Where a request to read the database now stands. */
export interface ReadState {
  /** When the last request was made, if its value reads as an instant. */
  requested?: Date
  /** The operator recorded the last request as answered. */
  answered: boolean
}

/**
 * The last request and whether the operator answered it. A request is
 * answered once status.lastReadRequest holds its value; the operator records
 * a value longer than 64 bytes as its digest, which the console's own
 * requests never are.
 */
export function readState(resource: ResourceView): ReadState | undefined {
  const request = (resource.schema ?? resource.migration)?.readRequest
  if (!request?.requested) {
    return undefined
  }
  const requested = new Date(request.requested)
  return {
    requested: Number.isNaN(requested.getTime()) ? undefined : requested,
    answered: request.answered === request.requested,
  }
}
