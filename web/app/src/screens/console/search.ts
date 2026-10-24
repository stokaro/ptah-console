import { activityPeriods, activityTypes, type ActivityFilter } from "../../model/activity"
import {
  pageSizes,
  resourceStates,
  sortOrders,
  type KindFilter,
  type SortOrder,
  type StateFilter,
} from "../../model/catalog"

/** The list's place in the address, so a filtered list is a link someone can send. */
export interface ResourcesSearch {
  q?: string
  kind?: KindFilter
  state?: StateFilter
  sort?: SortOrder
  page?: number
  size?: number
}

const kinds: KindFilter[] = ["all", "schema", "migration"]
const states: StateFilter[] = ["all", ...resourceStates.map((s) => s.value)]
const sorts: SortOrder[] = sortOrders.map((s) => s.value)

function oneOf<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return allowed.find((candidate) => candidate === value)
}

/**
 * Reads the list's state out of the address. Anything it does not know is
 * dropped rather than refused: a stale link opens the plain list.
 */
export function readResourcesSearch(search: Record<string, unknown>): ResourcesSearch {
  const page = Number(search.page)
  const size = Number(search.size)
  return {
    q: typeof search.q === "string" && search.q !== "" ? search.q : undefined,
    kind: oneOf(search.kind, kinds),
    state: oneOf(search.state, states),
    sort: oneOf(search.sort, sorts),
    page: Number.isInteger(page) && page > 1 ? page : undefined,
    size: pageSizes.find((candidate) => candidate === size),
  }
}

const activityTypeValues = activityTypes.map((type) => type.value)
const activityPeriodValues = activityPeriods.map((period) => period.value)

/** Reads the activity filters out of the address; anything unknown is dropped. */
export function readActivitySearch(search: Record<string, unknown>): ActivityFilter {
  return {
    type: oneOf(search.type, activityTypeValues),
    period: oneOf(search.period, activityPeriodValues),
    who: typeof search.who === "string" && search.who !== "" ? search.who : undefined,
  }
}
