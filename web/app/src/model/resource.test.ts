import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { StageState, StatementSchema } from "../gen/ptah/console/v1/resources_pb"
import { waitingSchema } from "../test/console"
import { csvField, fromSchema, say, statementTag } from "./resource"

describe("csvField", () => {
  it.each([
    ["plain", "plain"],
    ["=1+1", "'=1+1"],
    ["+cmd", "'+cmd"],
    ["-2", "'-2"],
    ["@SUM(A1)", "'@SUM(A1)"],
    ["a,b", '"a,b"'],
    ['say "hi"', '"say ""hi"""'],
  ])("%s becomes %s", (value, want) => {
    expect(csvField(value)).toBe(want)
  })
})

describe("statementTag", () => {
  it.each([
    ["safe", "", undefined, undefined],
    ["destructive", "drops a column", "drops a column", "bad"],
    ["warning", "", "warning", "warn"],
    ["", "", undefined, undefined],
  ])("severity %s", (severity, reason, tag, tone) => {
    expect(statementTag(create(StatementSchema, { severity, reason, sql: "x" }))).toEqual(
      tag ? { tag, tone } : {}
    )
  })
})

describe("say", () => {
  it("speaks of the attention first", () => {
    expect(say(fromSchema(waitingSchema()), new Date())).toEqual({
      tone: "warn",
      text: "Waiting for an approver. Nothing has changed in the database yet.",
    })
  })

  it("calls a resource verified only when every stage is done", () => {
    const done = fromSchema(
      waitingSchema((schema) => {
        schema.attention = []
        for (const stage of schema.stages) {
          stage.state = StageState.DONE
          stage.detail = ""
        }
      })
    )
    const applying = fromSchema(
      waitingSchema((schema) => {
        schema.attention = []
        schema.stages[3].state = StageState.ACTIVE
        schema.stages[3].detail = "The executor Job runs the plan."
      })
    )

    expect(say(done, new Date()).tone).toBe("ok")
    expect(say(applying, new Date())).toEqual({
      tone: "run",
      text: "The executor Job runs the plan.",
    })
  })
})

// One sentence for each combination of stages a schema passes through.
describe("say across stage combinations", () => {
  const stages = (...states: StageState[]) =>
    fromSchema(
      waitingSchema((schema) => {
        schema.attention = []
        schema.stages.forEach((stage, index) => {
          stage.state = states[index] ?? StageState.NOT_APPLICABLE
          stage.detail = ""
        })
      })
    )

  it.each([
    { name: "planning", states: [StageState.DONE, StageState.ACTIVE], tone: "run" },
    {
      name: "applying",
      states: [StageState.DONE, StageState.DONE, StageState.DONE, StageState.ACTIVE],
      tone: "run",
    },
    {
      name: "verified",
      states: [StageState.DONE, StageState.DONE, StageState.DONE, StageState.DONE, StageState.DONE],
      tone: "ok",
    },
    {
      name: "nothing to apply",
      states: [
        StageState.DONE,
        StageState.DONE,
        StageState.NOT_APPLICABLE,
        StageState.NOT_APPLICABLE,
        StageState.DONE,
      ],
      tone: "ok",
    },
    {
      name: "an Apply that failed",
      states: [StageState.DONE, StageState.DONE, StageState.DONE, StageState.FAILED],
      tone: "bad",
    },
  ])("$name", ({ states, tone }) => {
    expect(say(stages(...states), new Date()).tone).toBe(tone)
  })
})
