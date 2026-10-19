import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { MigrationSchema, StageState } from "../gen/ptah/console/v1/resources_pb"
import { fromMigration, fromSchema } from "../model/resource"
import { waitingSchema } from "../test/console"
import { suspendText } from "./ReleaseDetail"

const migration = (apply: StageState) =>
  fromMigration(
    create(MigrationSchema, {
      ref: { namespace: "application", name: "billing" },
      stages: [{ name: "Apply", state: apply }],
    })
  )

describe("suspendText", () => {
  it("says a spec change retires the current plan", () => {
    expect(suspendText(fromSchema(waitingSchema()), true)).toMatch(/retires the current plan/)
  })

  it("warns that suspending a running migration leaves a run to acknowledge", () => {
    expect(suspendText(migration(StageState.ACTIVE), true)).toMatch(
      /a person has to acknowledge it/
    )
    expect(suspendText(migration(StageState.DONE), true)).not.toMatch(/acknowledge/)
  })

  it("says resuming plans again before applying", () => {
    expect(suspendText(migration(StageState.ACTIVE), false)).toMatch(/plans before it applies/)
  })
})
