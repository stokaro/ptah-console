import {
  Access,
  Answer,
  type Capabilities,
  type NamespaceScope,
} from "../gen/ptah/console/v1/resources_pb"

/**
 * The namespaces a viewer chose to work in: every readable one, so that a
 * namespace granted later joins, or the ones named.
 */
export type ScopeChoice = { all: true } | { all: false; namespaces: string[] }

const namespaceName = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/

/** The stored form's version; a choice stored in another is no choice. */
const version = 1

/** The key a choice is kept under: one per cluster and identity, never shared between them. */
export function choiceKey(cluster: string, username: string) {
  return `ptah-console-scope:${version}:${cluster}:${username}`
}

/** Reads a stored choice. Anything that is not one, or nothing, is no choice. */
export function parseChoice(raw: string | null): ScopeChoice | undefined {
  if (raw === null) {
    return undefined
  }
  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    return undefined
  }
  if (typeof value !== "object" || value === null) {
    return undefined
  }
  const record = value as Record<string, unknown>
  if (record.all === true) {
    return { all: true }
  }
  const names = record.namespaces
  if (
    record.all !== false ||
    !Array.isArray(names) ||
    names.length === 0 ||
    !names.every((name) => typeof name === "string" && namespaceName.test(name))
  ) {
    return undefined
  }
  return { all: false, namespaces: [...new Set(names as string[])] }
}

export function serializeChoice(choice: ScopeChoice) {
  return JSON.stringify(choice.all ? { all: true } : { all: false, namespaces: choice.namespaces })
}

/**
 * The choice a selection makes. Selecting every readable namespace is
 * choosing all of them, so one granted later joins; a selection of none is
 * no choice at all.
 */
export function choiceOf(selection: Iterable<string>, readable: string[]): ScopeChoice | undefined {
  const chosen = new Set(selection)
  const names = readable.filter((name) => chosen.has(name))
  if (names.length === 0) {
    return undefined
  }
  return names.length === readable.length ? { all: true } : { all: false, namespaces: names }
}

export interface InScope {
  /** The namespaces the pages read, in the order the server offered them. */
  namespaces: string[]
  /** Fewer namespaces than the identity may read are shown. */
  narrowed: boolean
  /**
   * The viewer chose namespaces of which none is readable now, so every
   * readable namespace is shown rather than none.
   */
  lapsed: boolean
}

/** What a choice selects among the namespaces readable now. */
export function inScope(readable: string[], choice: ScopeChoice | undefined): InScope {
  if (choice === undefined || choice.all) {
    return { namespaces: readable, narrowed: false, lapsed: false }
  }
  const chosen = new Set(choice.namespaces)
  const namespaces = readable.filter((name) => chosen.has(name))
  if (namespaces.length === 0) {
    return { namespaces: readable, narrowed: false, lapsed: readable.length > 0 }
  }
  return { namespaces, narrowed: namespaces.length < readable.length, lapsed: false }
}

/** The names that contain the search, ignoring case and the spaces around it. */
export function matching(names: string[], query: string) {
  const wanted = query.trim().toLowerCase()
  return wanted === "" ? names : names.filter((name) => name.toLowerCase().includes(wanted))
}

type Capability = Exclude<keyof Capabilities, "$typeName" | "$unknown">

function allowed(capabilities: Capabilities | undefined, ...keys: Capability[]) {
  return keys.some((key) => capabilities?.[key] === Answer.ALLOWED)
}

/** What the identity may do in a namespace, in a few words for the picker. */
export function accessLabel(scope: NamespaceScope) {
  switch (scope.access) {
    case Access.OK:
      return allowed(
        scope.capabilities,
        "approveSchemaPlans",
        "approveMigrationPlans",
        "acknowledgeRuns"
      )
        ? "may read and decide"
        : "may read"
    case Access.FORBIDDEN:
      return "may not read"
    default:
      return "access unknown"
  }
}

/** "3 namespaces", or "2 of the 5 namespaces" when the viewer narrowed them. */
export function namespacesRead(read: number, readable: number) {
  if (read < readable) {
    return `${read} of the ${readable} namespaces`
  }
  return `${readable} ${readable === 1 ? "namespace" : "namespaces"}`
}
