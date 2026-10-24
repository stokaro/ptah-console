import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { MigrationSchema, SchemaSchema } from "../gen/ptah/console/v1/resources_pb"
import { fromMigration, fromSchema } from "./resource"
import { filterRuns, migrationRuns, runCounts, timedRuns } from "./runs"

const at = (iso: string) => timestampFromDate(new Date(iso))

const migration = fromMigration(
  create(MigrationSchema, {
    ref: { namespace: "application", name: "billing", uid: "uid-migration" },
    journal: {
      coveredSince: at("2026-10-01T00:00:00Z"),
      entries: [
        {
          type: "RunFinished",
          startedAt: at("2026-10-20T10:00:00Z"),
          at: at("2026-10-20T10:00:40Z"),
          outcome: "Succeeded",
          appliedVersions: 2,
        },
        { type: "PlanPublished", at: at("2026-10-21T09:00:00Z"), plan: "billing-p-2" },
        {
          type: "RunFinished",
          startedAt: at("2026-10-21T10:00:00Z"),
          at: at("2026-10-21T10:03:00Z"),
          outcome: "Failed",
          failure: { version: 16n, statement: 2, statements: 3, sqlState: "23505" },
        },
        { type: "RunFinished", at: at("2026-10-22T10:00:00Z"), outcome: "Unknown" },
        {
          type: "RunFinished",
          startedAt: at("2026-10-23T10:00:00Z"),
          at: at("2026-10-23T10:00:05Z"),
          outcome: "UpToDate",
        },
      ],
    },
  })
)

describe("A migration's runs", () => {
  it("are the runs the journal records, newest first, and nothing else it records", () => {
    expect(migrationRuns(migration).map((run) => run.title)).toEqual([
      "Run found nothing to apply",
      "Run outcome unknown",
      "Run failed at migration 16, statement 2 of 3, SQLSTATE 23505",
      "Run applied 2 versions",
    ])
  })

  it("are none for a schema, whose Applies are not runs", () => {
    const schema = fromSchema(
      create(SchemaSchema, {
        journal: {
          entries: [
            { type: "ApplyFinished", at: at("2026-10-20T10:00:00Z"), outcome: "Succeeded" },
          ],
        },
      })
    )
    expect(migrationRuns(schema)).toEqual([])
  })

  it("filter by how they ended, and count each way", () => {
    const runs = migrationRuns(migration)
    expect(filterRuns(runs, "failed").map((run) => run.title)).toEqual([
      "Run failed at migration 16, statement 2 of 3, SQLSTATE 23505",
    ])
    expect(runCounts(runs)).toEqual({ all: 4, succeeded: 2, failed: 1, unknown: 1 })
  })

  it("chart oldest first, and leave out a run that recorded no start", () => {
    expect(timedRuns(migrationRuns(migration)).map((run) => run.durationMs)).toEqual([
      40_000, 180_000, 5_000,
    ])
  })
})
