import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { AttentionKind, AttentionSchema, StageState } from "../gen/ptah/console/v1/resources_pb"
import { minutesAgo, syncedMigration, waitingSchema } from "../test/console"
import {
  matchesText,
  paginate,
  select,
  stateLabel,
  stateOf,
  updatedAt,
  type CatalogQuery,
} from "./catalog"
import { fromMigration, fromSchema } from "./resource"

const waiting = fromSchema(waitingSchema())
const synced = fromMigration(syncedMigration())

const applying = fromSchema(
  waitingSchema((schema) => {
    schema.ref!.name = "inventory"
    schema.ref!.uid = "uid-inventory"
    schema.attention = []
    schema.stages[2].state = StageState.DONE
    schema.stages[3].state = StageState.ACTIVE
    schema.conditions = [{ ...schema.conditions[0], lastTransition: minutesAgo(1) }]
  })
)

const suspendedWhileWaiting = fromSchema(
  waitingSchema((schema) => {
    schema.ref!.name = "payroll"
    schema.ref!.uid = "uid-payroll"
    schema.suspended = true
  })
)

const unread = fromSchema(
  waitingSchema((schema) => {
    schema.ref!.name = "reports"
    schema.ref!.uid = "uid-reports"
    schema.attention = []
    schema.stages = []
    schema.conditions = []
    schema.phase = ""
  })
)

const all = [synced, waiting, applying, suspendedWhileWaiting, unread]

const everything: CatalogQuery = { q: "", kind: "all", state: "all", sort: "attention" }

describe("stateOf", () => {
  it.each([
    ["waiting for an approval", waiting, "attention"],
    ["a stage under way", applying, "progress"],
    ["every stage done", synced, "insync"],
    ["suspended while it waits", suspendedWhileWaiting, "suspended"],
    ["no stage at all", unread, "none"],
  ])("files %s as %s", (_, resource, state) => {
    expect(stateOf(resource)).toBe(state)
  })

  it("does not call a resource in sync while one stage is not done", () => {
    const planned = fromMigration(
      syncedMigration((migration) => {
        migration.stages[4].state = StageState.WAITING
      })
    )
    expect(stateOf(planned)).toBe("none")
  })

  it("files a suspension that arrives as attention as suspended", () => {
    const flagged = fromMigration(
      syncedMigration((migration) => {
        migration.attention = [
          create(AttentionSchema, { kind: AttentionKind.SUSPENDED, reason: "Suspended by alice." }),
        ]
      })
    )
    expect(stateOf(flagged)).toBe("suspended")
  })
})

describe("stateLabel", () => {
  it("names the kind of attention", () => {
    expect(stateLabel(waiting)).toEqual({ tone: "warn", text: "Waiting for approval" })
  })

  it("names the phase when nothing else is known", () => {
    const phased = fromSchema(
      waitingSchema((schema) => {
        schema.attention = []
        schema.stages = []
        schema.phase = "Pending"
      })
    )
    expect(stateLabel(phased)).toEqual({ tone: "off", text: "Pending" })
    expect(stateLabel(unread)).toEqual({ tone: "off", text: "No status" })
  })
})

describe("matchesText", () => {
  it.each([
    ["the name", "STORE"],
    ["the namespace", "applic"],
    ["the artifact", "registry.example/schemas"],
    ["the digest", "5f2c0000"],
    ["why it waits", "an approver"],
  ])("finds a resource by %s", (_, text) => {
    expect(matchesText(waiting, text)).toBe(true)
  })

  it("refuses text that names none of them", () => {
    expect(matchesText(waiting, "billing")).toBe(false)
    expect(matchesText(waiting, "registry.example/migrations")).toBe(false)
  })
})

describe("updatedAt", () => {
  it("is the newest condition transition", () => {
    expect(updatedAt(applying)!.getTime()).toBeGreaterThan(updatedAt(waiting)!.getTime())
  })

  it("is unknown without a dated condition", () => {
    expect(updatedAt(unread)).toBeUndefined()
  })
})

describe("select", () => {
  it("puts what needs a person first, and what is in sync last", () => {
    const { rows } = select(all, everything)
    expect(rows.map((r) => r.name)).toEqual([
      "storefront",
      "inventory",
      "reports",
      "payroll",
      "billing",
    ])
  })

  it("sorts by the newest change, an undated resource last", () => {
    const { rows } = select(all, { ...everything, sort: "updated" })
    expect(rows[0].name).toBe("inventory")
    expect(rows.at(-1)!.name).toBe("reports")
  })

  it("narrows by kind and by state together", () => {
    expect(select(all, { ...everything, kind: "migration" }).rows.map((r) => r.name)).toEqual([
      "billing",
    ])
    expect(select(all, { ...everything, kind: "schema", state: "insync" }).rows).toHaveLength(0)
  })

  it("counts each filter with the others applied", () => {
    const { kindCounts, stateCounts } = select(all, { ...everything, kind: "schema" })
    expect(kindCounts).toEqual({ all: 5, schema: 4, migration: 1 })
    expect(stateCounts).toEqual({
      all: 4,
      attention: 1,
      progress: 1,
      insync: 0,
      suspended: 1,
      none: 1,
    })
  })
})

describe("paginate", () => {
  const rows = Array.from({ length: 60 }, (_, index) => index)

  it("says which rows a page holds", () => {
    expect(paginate(rows, 2, 25)).toMatchObject({
      page: 2,
      pages: 3,
      first: 26,
      last: 50,
      total: 60,
    })
    expect(paginate(rows, 3, 25).rows).toHaveLength(10)
  })

  it("clamps a page the rows no longer reach", () => {
    expect(paginate(rows, 9, 25).page).toBe(3)
    expect(paginate(rows, 0, 25).page).toBe(1)
    expect(paginate([], 4, 25)).toMatchObject({ page: 1, pages: 1, first: 0, last: 0, total: 0 })
  })
})
