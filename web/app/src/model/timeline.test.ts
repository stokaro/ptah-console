import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import {
  MigrationHistorySchema,
  MigrationPlanSchema,
  MigrationRevisionSchema,
  MigrationRunSchema,
  MigrationSchema,
} from "../gen/ptah/console/v1/resources_pb"
import { migrationTimeline, runTime, unlistedApplied } from "./timeline"

function migration(outcome = "Failed") {
  return create(MigrationSchema, {
    history: create(MigrationHistorySchema, {
      currentVersion: 4n,
      appliedCount: 5,
      dirty: true,
      outOfOrderVersions: [2n],
      revisions: [
        create(MigrationRevisionSchema, { version: 4n, description: "add index", state: "dirty" }),
        create(MigrationRevisionSchema, {
          version: 3n,
          description: "add totals",
          state: "modified",
        }),
        create(MigrationRevisionSchema, { version: 1n, description: "create", state: "applied" }),
      ],
    }),
    lastRun: create(MigrationRunSchema, {
      outcome,
      failure: { version: 5n, statement: 1, statements: 1, sqlState: "42P01" },
    }),
  })
}

const plan = create(MigrationPlanSchema, {
  migrations: [
    { version: 4n, versionKey: "0004", description: "add index" },
    { version: 6n, versionKey: "0006", description: "add view" },
  ],
})

describe("A migration's timeline", () => {
  it("lists each version once, newest first, by what the database says of it first", () => {
    const steps = migrationTimeline(migration(), plan)

    expect(steps.map((step) => [step.label, step.state])).toEqual([
      ["0006", "held"],
      ["5", "failed"],
      ["4", "dirty"],
      ["3", "modified"],
      ["2", "out-of-order"],
      ["1", "applied"],
    ])
  })

  it("names the step a run stopped in by its file, where the plan has it", () => {
    const stopped = migration()
    stopped.lastRun!.failure!.version = 6n

    expect(migrationTimeline(stopped, plan)[0]).toMatchObject({
      label: "6",
      name: "add view",
      state: "failed",
    })
  })

  it("names the failed step only for a run that stopped", () => {
    const steps = migrationTimeline(migration("Applied"), plan)

    expect(steps.map((step) => step.label)).not.toContain("5")
  })

  it("names a pending file as waiting once the history is clean", () => {
    const clean = migration()
    clean.history!.dirty = false

    expect(migrationTimeline(clean, plan)[0]).toMatchObject({
      label: "0006",
      state: "pending",
      note: "In the plan, waiting to apply",
    })
  })

  it("counts the applied migrations the history holds and the rows leave out", () => {
    expect(unlistedApplied(migration())).toBe(4)
    expect(unlistedApplied(create(MigrationSchema, {}))).toBe(0)
  })
})

describe("How long a migration ran", () => {
  it.each([
    [0, "0 ms"],
    [999, "999 ms"],
    [1000, "1.0 s"],
    [1840, "1.8 s"],
    [59_940, "59.9 s"],
    [61_000, "1 min"],
  ])("reads %i ms as %s", (ms, says) => {
    expect(runTime(ms)).toBe(says)
  })
})
