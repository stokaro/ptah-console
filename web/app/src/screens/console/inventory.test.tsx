import { create } from "@bufbuild/protobuf"
import { screen, within } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { TargetSchema, type Target } from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, waitingSchema } from "../../test/console"

function renderTarget(target: Target) {
  return renderConsole("/console/schemas/application/storefront", {
    schemas: [
      waitingSchema((s) => {
        s.target = target
      }),
    ],
  })
}

const rows = (name: string) =>
  within(screen.getByRole("table", { name }))
    .getAllByRole("row")
    .slice(1)
    .map((row) => row.textContent)

const items = (name: string) =>
  within(screen.getByRole("list", { name }))
    .getAllByRole("listitem")
    .map((item) => item.textContent)

// A legend describes a drawing a screen reader skips, so it is hidden with it.
const legend = (card: string) => {
  const list = screen.getByRole("region", { name: card }).querySelector("ul[aria-hidden='true']")
  return Array.from(list?.querySelectorAll("li") ?? []).map((item) => item.textContent)
}

describe("A schema's desired and observed state", () => {
  it("shows each side's counts, the two compared by kind, and what differs", async () => {
    const { container } = renderTarget(
      create(TargetSchema, {
        engine: "PostgreSQL",
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "destructive",
        driftFindingCount: 3,
        driftFindings: [
          { category: "columns_removed", count: 2, severity: "destructive" },
          { category: "tables_added", count: 1, severity: "safe" },
        ],
        inventory: [
          { kind: "columns", desired: 85, observed: 87 },
          { kind: "tables", desired: 12, observed: 11 },
        ],
      })
    )

    await screen.findByRole("list", { name: "Objects declared in the schema" })
    expect(items("Objects declared in the schema")).toEqual(["Columns85", "Tables12"])
    expect(screen.getByRole("region", { name: "Desired state" })).toHaveTextContent(
      "97 objects counted in the schema"
    )
    expect(screen.getByRole("region", { name: "Observed state" })).toHaveTextContent(
      "98 objects counted in the database, in the compared scope"
    )
    // Kind, declared, held.
    expect(rows("Declared and held objects of storefront")).toEqual(["Columns8587", "Tables1211"])
    expect(legend("Schema comparison")).toEqual(["Declared in the schema", "Held by the database"])
    const drift = screen.getByRole("region", { name: "Object drift" })
    expect(drift).toHaveTextContent(
      "×The last read found 3 drift findings, the most severe destructive."
    )
    // A safe difference is still one: it never reads as done.
    expect(items("Kinds that differ")).toEqual([
      "×Columns2 only in the database · destructive",
      "!Tables1 only declared · safe",
    ])
    expect(screen.queryByRole("list", { name: "Other findings" })).toBeNull()
    // Two kinds make no radar: the coverage is a table anyone can read.
    expect(rows("Coverage per kind of storefront")).toEqual([
      "Columns85 of 85100%",
      "Tables11 of 1291%",
    ])
    expect(screen.getByRole("region", { name: "Schema profile" }).querySelector("svg")).toBeNull()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("says one object in the singular, and lists no kind a side does not hold", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "safe",
        driftFindingCount: 1,
        driftFindings: [{ category: "tables_added", count: 1, severity: "safe" }],
        inventory: [{ kind: "tables", desired: 1, observed: 0 }],
      })
    )

    expect(await screen.findByRole("region", { name: "Desired state" })).toHaveTextContent(
      "1 object counted in the schema"
    )
    const observed = screen.getByRole("region", { name: "Observed state" })
    expect(observed).toHaveTextContent("0 objects counted in the database")
    expect(observed).toHaveTextContent("None.")
    expect(screen.queryByRole("list", { name: "Objects found in the database" })).toBeNull()
    expect(screen.getByRole("region", { name: "Object drift" })).toHaveTextContent(
      "!The last read found 1 drift finding, the most severe safe."
    )
  })

  it("says nothing differs only when the read found no drift at all", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        inventory: [
          { kind: "columns", desired: 3, observed: 3 },
          { kind: "tables", desired: 1, observed: 1 },
        ],
      })
    )

    const drift = await screen.findByRole("region", { name: "Object drift" })
    expect(drift).toHaveTextContent("✓No object differs between the schema and the database.")
    expect(screen.queryByRole("list", { name: "Kinds that differ" })).toBeNull()
    expect(drift).not.toHaveTextContent(/finding/)
  })

  it("does not call drift in no category a match", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        // The report found drift it lists under no category: it reads safe,
        // with no finding and a zero count.
        highestDriftSeverity: "safe",
        inventory: [{ kind: "tables", desired: 1, observed: 1 }],
      })
    )

    const drift = await screen.findByRole("region", { name: "Object drift" })
    expect(drift).toHaveTextContent(
      "!The last read found drift from the desired schema that no category names; the plan says what it changes."
    )
    expect(drift).not.toHaveTextContent("No object differs")
  })

  it("does not say nothing differs beside a finding a reading without a severity lists", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        driftFindings: [{ category: "tables_added", count: 1, severity: "safe" }],
        inventory: [{ kind: "tables", desired: 2, observed: 1 }],
      })
    )

    const drift = await screen.findByRole("region", { name: "Object drift" })
    expect(items("Kinds that differ")).toEqual(["!Tables1 only declared · safe"])
    expect(drift).not.toHaveTextContent("No object differs")
  })

  it("puts a change of a kind's own on the kind", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "warning",
        driftFindingCount: 1,
        driftFindings: [{ category: "functions_modified", count: 1, severity: "warning" }],
        inventory: [
          { kind: "functions", desired: 3, observed: 3 },
          { kind: "tables", desired: 1, observed: 1 },
        ],
      })
    )

    await screen.findByRole("region", { name: "Object drift" })
    expect(items("Kinds that differ")).toEqual(["!Functions1 changed · warning"])
    expect(screen.getByText("The other kind has no finding.")).toBeVisible()
  })

  it("puts a finding about a kind's objects on the kind, and lists the rest by category", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "destructive",
        driftFindingCount: 5,
        driftFindings: [
          { category: "indexes_removed", count: 1, severity: "warning" },
          { category: "unique_protections_removed", count: 1, severity: "destructive" },
          { category: "indexes_renamed", count: 1, severity: "warning" },
          { category: "comments_changed", count: 2, severity: "safe" },
        ],
        inventory: [
          { kind: "indexes", desired: 2, observed: 3 },
          { kind: "tables", desired: 1, observed: 1 },
          { kind: "columns", desired: 4, observed: 4 },
        ],
      })
    )

    const drift = await screen.findByRole("region", { name: "Object drift" })
    // The removal enforced uniqueness, so the kind rates destructive although
    // indexes_removed rates it a warning.
    expect(items("Kinds that differ")).toEqual([
      "×Indexes1 only in the database · 1 removed that enforced uniqueness · 1 renamed · destructive",
    ])
    expect(items("Other findings")).toEqual(["!comments_changed2 · safe"])
    expect(drift).toHaveTextContent("The other 2 kinds have no finding.")
    expect(drift).not.toHaveTextContent(/match|changed\./)
  })

  it("says no counted kind has a finding when every finding is listed by category", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "safe",
        driftFindingCount: 1,
        driftFindings: [{ category: "comments_changed", count: 1, severity: "safe" }],
        inventory: [{ kind: "tables", desired: 1, observed: 1 }],
      })
    )

    const drift = await screen.findByRole("region", { name: "Object drift" })
    expect(screen.queryByRole("list", { name: "Kinds that differ" })).toBeNull()
    expect(items("Other findings")).toEqual(["!comments_changed1 · safe"])
    expect(drift).toHaveTextContent("No counted kind has a finding.")
  })

  it("names a kind whose counts and findings disagree instead of calling it a match", async () => {
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "safe",
        driftFindingCount: 5,
        driftFindings: [{ category: "tables_added", count: 5, severity: "safe" }],
        inventory: [
          { kind: "tables", desired: 12, observed: 11 },
          { kind: "columns", desired: 3, observed: 3 },
        ],
      })
    )

    await screen.findByRole("region", { name: "Object drift" })
    expect(items("Kinds that differ")).toEqual(["?Tablesits counts and its findings disagree"])
    expect(screen.getByText("The other kind has no finding.")).toBeVisible()
    // Its coverage is not known, so only columns has one.
    expect(rows("Coverage per kind of storefront")).toEqual(["Columns3 of 3100%"])
  })

  it("draws the coverage of three to eight kinds as a radar, and says it in a table", async () => {
    const { container } = renderTarget(
      create(TargetSchema, {
        engine: "PostgreSQL",
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "warning",
        driftFindingCount: 1,
        driftFindings: [{ category: "indexes_added", count: 1, severity: "warning" }],
        inventory: [
          { kind: "tables", desired: 12, observed: 12 },
          { kind: "columns", desired: 85, observed: 85 },
          { kind: "indexes", desired: 4, observed: 3 },
          { kind: "rls_policies", desired: 0, observed: 0 },
        ],
      })
    )

    const profile = await screen.findByRole("region", { name: "Schema profile" })
    const radar = profile.querySelector("svg")
    expect(radar).toHaveAttribute("aria-hidden", "true")
    // Four rings, the declared outline and the database's.
    expect(radar?.querySelectorAll("polygon")).toHaveLength(6)
    // Neither series is drawn in the accent, which a viewer can set to teal.
    expect(radar?.innerHTML).not.toMatch(/pri\b/)
    expect(rows("Coverage per kind of storefront")).toEqual([
      "Tables12 of 12100%",
      "Columns85 of 85100%",
      "Indexes3 of 475%",
    ])
    expect(legend("Schema profile")).toEqual(["Declared, every one", "Held by the database"])
    expect(screen.getByText("The other 3 kinds have no finding.")).toBeVisible()
    expect(await axe(container)).toHaveNoViolations()
  })

  it.each([
    [8, true],
    [9, false],
  ])("draws %i kinds as a radar: %s", async (count, drawn) => {
    const kinds = [
      "schemas",
      "tables",
      "columns",
      "indexes",
      "constraints",
      "functions",
      "views",
      "sequences",
      "triggers",
    ].slice(0, count)
    renderTarget(
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        inventory: kinds.map((kind) => ({ kind, desired: 2, observed: 2 })),
      })
    )

    const profile = await screen.findByRole("region", { name: "Schema profile" })
    expect(profile.querySelector("svg") !== null).toBe(drawn)
    expect(rows("Coverage per kind of storefront")).toHaveLength(count)
  })

  it("says the operator published no counts, and why that can be, after a read", async () => {
    renderTarget(create(TargetSchema, { engine: "PostgreSQL", lastObservedAt: minutesAgo(5) }))

    expect(
      await screen.findByText(
        /^The last read published no object counts\. Either neither side holds an object the comparison counts, or the operator or its executor does not count them/
      )
    ).toBeVisible()
    expect(screen.queryByRole("region", { name: "Schema comparison" })).not.toBeInTheDocument()
  })

  it("says nothing is counted before the first read", async () => {
    renderConsole("/console/schemas/application/storefront", { schemas: [waitingSchema()] })

    expect(
      await screen.findByText("The database is not read yet, so no object is counted.")
    ).toBeVisible()
    expect(screen.queryByRole("region", { name: "Schema comparison" })).not.toBeInTheDocument()
  })
})
