import type { DriftFinding, Target } from "../gen/ptah/console/v1/resources_pb"

import { worseSeverity } from "./drift"

/** One kind of object as the last observation counted it on both sides. */
export interface KindRow {
  /** The stem of the kind's drift categories, such as rls_policies. */
  kind: string
  label: string
  desired: number
  observed: number
  /** Declared and not in the database: the kind's _added findings. */
  onlyDesired: number
  /** In the database and not declared: the kind's _removed findings. */
  onlyObserved: number
  /** Changed on both sides: the kind's _modified findings, where it has them. */
  modified: number
  /**
   * The findings that describe objects of the kind without moving its
   * counts, each with a phrase to follow its count: "2 renamed".
   */
  notes: { category: string; count: number; phrase: string }[]
  /** The most severe of all its findings, or empty when the kind has none. */
  severity: string
  /**
   * The objects both sides hold. The counts and the findings come from one
   * comparison, so desired less the additions equals observed less the
   * removals; a pair that disagrees leaves it unknown rather than guessed.
   */
  matched?: number
}

// The findings a kind owns: its stem and one of these.
const ownSuffixes = ["_added", "_removed", "_modified"] as const

// The categories Ptah v0.16.0 counts beside a kind's own, which describe
// objects of that kind without moving its counts (migration/safety's
// objectKinds): a change to a pair both sides hold, or a part of a removal the
// kind counts already. Each phrase follows a count. A category missing here
// is listed on its own, so a new one Ptah adds is shown, not lost.
const describing: Record<string, { kind: string; phrase: string }> = {
  indexes_renamed: { kind: "indexes", phrase: "renamed" },
  index_visibility_changed: { kind: "indexes", phrase: "with visibility changed" },
  unique_protections_removed: { kind: "indexes", phrase: "removed that enforced uniqueness" },
  constraints_validated: { kind: "constraints", phrase: "to validate" },
  grant_options_added: { kind: "grants", phrase: "gaining the grant option" },
  grant_options_removed: { kind: "grants", phrase: "losing the grant option" },
  vector_dimension_changed: { kind: "columns", phrase: "with a changed vector dimension" },
  rls_force_added: { kind: "tables", phrase: "with FORCE row level security turned on" },
  rls_force_removed: { kind: "tables", phrase: "with FORCE row level security turned off" },
}

const labels: Record<string, string> = {
  rls_policies: "RLS policies",
  rls_enabled_tables: "Tables with RLS on",
  enum_values: "Enum values",
  role_memberships: "Role memberships",
  materialized_views: "Materialized views",
}

/** A kind's stem in words: rls_policies reads "RLS policies". */
export function kindLabel(kind: string) {
  const words = labels[kind] ?? kind.replaceAll("_", " ")
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/**
 * The kinds the last observation compared, in the order the operator
 * published them, each joined to the drift findings it owns.
 */
export function inventoryRows(target: Target | undefined): KindRow[] {
  const findings = new Map((target?.driftFindings ?? []).map((f) => [f.category, f]))
  return (target?.inventory ?? []).map((entry) => {
    const [added, removed, changed] = ownSuffixes.map((suffix) =>
      findings.get(`${entry.kind}${suffix}`)
    )
    const described = (target?.driftFindings ?? []).filter(
      (finding) => describing[finding.category]?.kind === entry.kind
    )
    const onlyDesired = added?.count ?? 0
    const onlyObserved = removed?.count ?? 0
    const fromDesired = entry.desired - onlyDesired
    const fromObserved = entry.observed - onlyObserved
    return {
      kind: entry.kind,
      label: kindLabel(entry.kind),
      desired: entry.desired,
      observed: entry.observed,
      onlyDesired,
      onlyObserved,
      modified: changed?.count ?? 0,
      notes: described.map((finding) => ({
        category: finding.category,
        count: finding.count,
        phrase: describing[finding.category].phrase,
      })),
      severity: [added, removed, changed, ...described].reduce(
        (worst, finding) => (finding ? worseSeverity(worst, finding.severity) : worst),
        ""
      ),
      matched: fromDesired === fromObserved && fromDesired >= 0 ? fromDesired : undefined,
    }
  })
}

/**
 * The findings that belong to no kind of the inventory, in the operator's
 * order: comments, reference data rows, objects withheld as undecided, a
 * feature owner's objects, which are not counted at all, and any category
 * this console does not place.
 */
export function otherFindings(target: Target | undefined): DriftFinding[] {
  const kinds = new Set((target?.inventory ?? []).map((entry) => entry.kind))
  return (target?.driftFindings ?? []).filter(
    (finding) =>
      !kinds.has(describing[finding.category]?.kind ?? "") &&
      !ownSuffixes.some(
        (suffix) =>
          finding.category.endsWith(suffix) && kinds.has(finding.category.slice(0, -suffix.length))
      )
  )
}

/** Whether a kind has a finding of its own or one about its objects, or counts that disagree. */
export function differs(row: KindRow): boolean {
  return (
    row.matched === undefined ||
    row.onlyDesired + row.onlyObserved + row.modified > 0 ||
    row.notes.length > 0
  )
}

/**
 * How many of the objects a kind declares the database holds, changed or
 * not: a share from 0 to 1 to draw, and a whole percentage rounded down, so
 * that 100% means every one.
 */
export function coverage(row: KindRow): { share: number; percent: number } | undefined {
  if (row.matched === undefined || row.desired === 0) {
    return undefined
  }
  return {
    share: row.matched / row.desired,
    percent: Math.floor((row.matched * 100) / row.desired),
  }
}

/** The totals over every kind: what the schema declares and what the database holds. */
export function inventoryTotals(rows: KindRow[]) {
  return rows.reduce(
    (total, row) => ({
      desired: total.desired + row.desired,
      observed: total.observed + row.observed,
    }),
    { desired: 0, observed: 0 }
  )
}

/** The kinds a coverage profile can draw: those that declare something and whose coverage is known. */
export function profileRows(rows: KindRow[]) {
  return rows.flatMap((row) => {
    const covered = coverage(row)
    return covered === undefined ? [] : [{ row, ...covered }]
  })
}

/**
 * The corners of a radar polygon: one value from 0 to 1 per axis, the first
 * axis straight up and the rest clockwise, around a center at radius 1.
 * Values outside the range are held to it.
 */
export function radarCorners(values: number[]): { x: number; y: number }[] {
  return values.map((value, index) => {
    const angle = (2 * Math.PI * index) / values.length - Math.PI / 2
    const r = Math.min(1, Math.max(0, value))
    return { x: r * Math.cos(angle), y: r * Math.sin(angle) }
  })
}
