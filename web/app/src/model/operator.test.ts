import { create } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { describe, expect, it } from "vitest"

import { OperationSchema, PendingObservationSchema } from "../gen/ptah/console/v1/resources_pb"
import { syncedMigration, waitingSchema } from "../test/console"
import { lateAfterMs, punctuality } from "./operator"
import { fromMigration, fromSchema } from "./resource"

const now = new Date("2026-10-24T12:00:00Z")
const at = (offsetMs: number) => timestampFromDate(new Date(now.getTime() + offsetMs))

function schemaDue(
  name: string,
  offsetMs: number,
  change: Parameters<typeof waitingSchema>[0] = () => {}
) {
  return fromSchema(
    waitingSchema((schema) => {
      schema.ref!.name = name
      schema.nextReconciliation = at(offsetMs)
      change(schema)
    })
  )
}

describe("punctuality", () => {
  it("counts the resources it can hold to a read, and finds none late on time", () => {
    const result = punctuality(
      [schemaDue("due-soon", 60_000), schemaDue("just-due", -lateAfterMs)],
      now
    )

    expect(result).toEqual({ judged: 2, late: [] })
  })

  it("names the resources past their read by more than the grace, the latest first", () => {
    const result = punctuality(
      [
        schemaDue("a-little-late", -lateAfterMs - 1_000),
        schemaDue("on-time", 0),
        schemaDue("very-late", -3_600_000),
      ],
      now
    )

    expect(result.judged).toBe(3)
    expect(result.late.map((resource) => resource.name)).toEqual(["very-late", "a-little-late"])
  })

  it("holds a migration to its read too", () => {
    const migration = fromMigration(
      syncedMigration((value) => {
        value.nextReconciliation = at(-3_600_000)
      })
    )

    expect(punctuality([migration], now).late).toEqual([migration])
  })

  it("does not judge what promised no read, or reads when its work ends", () => {
    const result = punctuality(
      [
        schemaDue("suspended", -3_600_000, (schema) => {
          schema.suspended = true
        }),
        schemaDue("running", -3_600_000, (schema) => {
          schema.activeOperation = create(OperationSchema, { type: "Plan" })
        }),
        schemaDue("proving", -3_600_000, (schema) => {
          schema.pendingObservation = create(PendingObservationSchema, {})
        }),
        fromMigration(
          syncedMigration((value) => {
            value.nextReconciliation = at(-3_600_000)
            value.activeOperation = create(OperationSchema, { type: "Apply" })
          })
        ),
        schemaDue("no-promise", 0, (schema) => {
          schema.nextReconciliation = undefined
        }),
      ],
      now
    )

    expect(result).toEqual({ judged: 0, late: [] })
  })
})
