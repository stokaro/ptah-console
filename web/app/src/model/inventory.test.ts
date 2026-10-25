import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { TargetSchema } from "../gen/ptah/console/v1/resources_pb"
import {
  coverage,
  differs,
  inventoryRows,
  inventoryTotals,
  kindLabel,
  otherFindings,
  profileRows,
  radarCorners,
} from "./inventory"

const target = create(TargetSchema, {
  driftFindings: [
    { category: "tables_added", count: 1, severity: "safe" },
    { category: "columns_removed", count: 2, severity: "destructive" },
    { category: "columns_modified", count: 1, severity: "warning" },
  ],
  inventory: [
    { kind: "columns", desired: 85, observed: 87 },
    { kind: "rls_policies", desired: 0, observed: 0 },
    { kind: "tables", desired: 12, observed: 11 },
  ],
})

describe("The object inventory", () => {
  it("joins each kind to the findings it owns, and counts what both sides hold", () => {
    expect(inventoryRows(target)).toEqual([
      {
        kind: "columns",
        label: "Columns",
        desired: 85,
        observed: 87,
        onlyDesired: 0,
        onlyObserved: 2,
        modified: 1,
        notes: [],
        severity: "destructive",
        matched: 85,
      },
      {
        kind: "rls_policies",
        label: "RLS policies",
        desired: 0,
        observed: 0,
        onlyDesired: 0,
        onlyObserved: 0,
        modified: 0,
        notes: [],
        severity: "",
        matched: 0,
      },
      {
        kind: "tables",
        label: "Tables",
        desired: 12,
        observed: 11,
        onlyDesired: 1,
        onlyObserved: 0,
        modified: 0,
        notes: [],
        severity: "safe",
        matched: 11,
      },
    ])
    expect(inventoryTotals(inventoryRows(target))).toEqual({ desired: 97, observed: 98 })
    expect(otherFindings(target)).toEqual([])
  })

  // Each of these names a kind's stem inside a longer one. None of them adds
  // to, takes from or describes the kind it looks like.
  it.each([
    ["rls_enabled_tables_added", "tables"],
    ["rls_enabled_tables_removed", "tables"],
    ["enum_values_added", "enums"],
    ["enum_values_removed", "enums"],
    ["table_constraints_added", "constraints"],
    ["comments_changed", "tables"],
  ])("leaves %s out of %s", (category, kind) => {
    const close = create(TargetSchema, {
      driftFindings: [{ category, count: 2, severity: "warning" }],
      inventory: [{ kind, desired: 5, observed: 5 }],
    })

    const [row] = inventoryRows(close)

    expect(row).toMatchObject({
      onlyDesired: 0,
      onlyObserved: 0,
      modified: 0,
      notes: [],
      severity: "",
      matched: 5,
    })
    expect(differs(row)).toBe(false)
    expect(otherFindings(close).map((finding) => finding.category)).toEqual([category])
  })

  // Each of these describes objects of a kind without moving its counts.
  it.each([
    ["indexes_renamed", "indexes", "renamed"],
    ["index_visibility_changed", "indexes", "with visibility changed"],
    ["unique_protections_removed", "indexes", "removed that enforced uniqueness"],
    ["constraints_validated", "constraints", "to validate"],
    ["grant_options_added", "grants", "gaining the grant option"],
    ["grant_options_removed", "grants", "losing the grant option"],
    ["vector_dimension_changed", "columns", "with a changed vector dimension"],
    ["rls_force_added", "tables", "with FORCE row level security turned on"],
    ["rls_force_removed", "tables", "with FORCE row level security turned off"],
  ])("puts %s on %s without moving its counts", (category, kind, phrase) => {
    const describing = create(TargetSchema, {
      driftFindings: [{ category, count: 2, severity: "destructive" }],
      inventory: [{ kind, desired: 5, observed: 5 }],
    })

    const [row] = inventoryRows(describing)

    expect(row).toMatchObject({
      onlyDesired: 0,
      onlyObserved: 0,
      modified: 0,
      notes: [{ category, count: 2, phrase }],
      severity: "destructive",
      matched: 5,
    })
    expect(differs(row)).toBe(true)
    expect(otherFindings(describing)).toEqual([])
  })

  it("lists a describing category on its own when its kind is not counted", () => {
    const uncounted = create(TargetSchema, {
      driftFindings: [{ category: "indexes_renamed", count: 1, severity: "warning" }],
      inventory: [{ kind: "tables", desired: 1, observed: 1 }],
    })

    expect(inventoryRows(uncounted)[0].notes).toEqual([])
    expect(otherFindings(uncounted).map((finding) => finding.category)).toEqual(["indexes_renamed"])
  })

  it("says a kind differs for a change of its own alone", () => {
    const [functions] = inventoryRows(
      create(TargetSchema, {
        driftFindings: [{ category: "functions_modified", count: 1, severity: "warning" }],
        inventory: [{ kind: "functions", desired: 3, observed: 3 }],
      })
    )

    expect(functions).toMatchObject({ modified: 1, matched: 3, severity: "warning" })
    expect(differs(functions)).toBe(true)
  })

  it("keeps the findings that belong to no kind, in the operator's order", () => {
    const mixed = create(TargetSchema, {
      driftFindings: [
        { category: "comments_changed", count: 1, severity: "safe" },
        { category: "tables_added", count: 1, severity: "safe" },
        { category: "triggers_added", count: 1, severity: "safe" },
        {
          category: "feature_changes:ptah.run/pgextension/extension-change",
          count: 1,
          severity: "warning",
        },
        { category: "data_rows_inserted", count: 3, severity: "safe" },
      ],
      // Neither side holds a trigger, so triggers_added has no kind to join.
      inventory: [{ kind: "tables", desired: 2, observed: 1 }],
    })

    expect(otherFindings(mixed).map((finding) => finding.category)).toEqual([
      "comments_changed",
      "triggers_added",
      "feature_changes:ptah.run/pgextension/extension-change",
      "data_rows_inserted",
    ])
  })

  it("rates a kind by the most severe finding about it", () => {
    const rated = create(TargetSchema, {
      driftFindings: [
        { category: "indexes_added", count: 1, severity: "warning" },
        { category: "indexes_removed", count: 1, severity: "warning" },
        { category: "unique_protections_removed", count: 1, severity: "destructive" },
        { category: "functions_modified", count: 1, severity: "warning" },
        { category: "functions_removed", count: 1, severity: "destructive" },
        { category: "functions_added", count: 1, severity: "safe" },
      ],
      inventory: [
        { kind: "functions", desired: 3, observed: 3 },
        { kind: "indexes", desired: 4, observed: 4 },
      ],
    })

    expect(inventoryRows(rated).map((row) => [row.kind, row.severity])).toEqual([
      ["functions", "destructive"],
      // An index removed with the uniqueness it enforced is a destructive
      // removal, though indexes_removed rates every removal a warning.
      ["indexes", "destructive"],
    ])
  })

  it("leaves the matched count unknown where the counts and the findings disagree", () => {
    const disagreeing = create(TargetSchema, {
      driftFindings: [{ category: "tables_added", count: 5, severity: "safe" }],
      inventory: [{ kind: "tables", desired: 12, observed: 11 }],
    })

    const [tables] = inventoryRows(disagreeing)

    expect(tables.matched).toBeUndefined()
    expect(coverage(tables)).toBeUndefined()
    expect(differs(tables)).toBe(true)
  })

  it("covers a kind by how many of the objects it declares the database holds, rounded down", () => {
    const [columns, policies, tables] = inventoryRows(target)

    expect(coverage(columns)).toEqual({ share: 1, percent: 100 })
    expect(coverage(policies)).toBeUndefined()
    expect(coverage(tables)?.percent).toBe(91)
    expect(coverage(tables)?.share).toBeCloseTo(11 / 12)
  })

  it.each([
    // 0.29 * 100 is 28.999999999999996 in floating point.
    [29, 100, 29],
    [199, 200, 99],
    [1, 3, 33],
    [2, 3, 66],
    [0, 4, 0],
  ])("reads %i of %i as %i%%", (held, declared, percent) => {
    const [row] = inventoryRows(
      create(TargetSchema, {
        driftFindings: [{ category: "views_added", count: declared - held, severity: "safe" }],
        inventory: [{ kind: "views", desired: declared, observed: held }],
      })
    )

    expect(coverage(row)?.percent).toBe(percent)
  })

  it("reads a stem as words", () => {
    expect(kindLabel("materialized_views")).toBe("Materialized views")
    expect(kindLabel("enum_values")).toBe("Enum values")
    expect(kindLabel("schemas")).toBe("Schemas")
  })
})

describe("The coverage profile", () => {
  it("draws the kinds that declare something and whose coverage is known", () => {
    const disagreeing = create(TargetSchema, {
      driftFindings: [
        { category: "tables_added", count: 1, severity: "safe" },
        { category: "indexes_added", count: 9, severity: "safe" },
      ],
      inventory: [
        { kind: "tables", desired: 12, observed: 11 },
        { kind: "rls_policies", desired: 0, observed: 3 },
        { kind: "indexes", desired: 4, observed: 4 },
      ],
    })

    expect(profileRows(inventoryRows(disagreeing))).toEqual([
      expect.objectContaining({ row: expect.objectContaining({ kind: "tables" }), percent: 91 }),
    ])
  })

  it("puts the first axis straight up and the rest clockwise, held to the unit circle", () => {
    const corners = radarCorners([1, 0.5, 2, -1])

    expect(corners[0].x).toBeCloseTo(0)
    expect(corners[0].y).toBeCloseTo(-1)
    expect(corners[1].x).toBeCloseTo(0.5)
    expect(corners[1].y).toBeCloseTo(0)
    expect(corners[2].x).toBeCloseTo(0)
    expect(corners[2].y).toBeCloseTo(1)
    expect(corners[3].x).toBeCloseTo(0)
    expect(corners[3].y).toBeCloseTo(0)
  })
})
