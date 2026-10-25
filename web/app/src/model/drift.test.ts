import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { TargetSchema } from "../gen/ptah/console/v1/resources_pb"
import { minutesAgo } from "../test/console"
import { differenceTone, driftPhrase, driftReading, readingTone, worseSeverity } from "./drift"

describe("A drift reading", () => {
  it.each([
    ["an unread database", create(TargetSchema, {}), { state: "unread" }, ""],
    [
      "a read with no drift",
      create(TargetSchema, { lastObservedAt: minutesAgo(5) }),
      { state: "none" },
      "no drift from the desired schema",
    ],
    [
      // A report lists some drift under no category: it reads safe with no
      // finding, and a zero count must not read as a match.
      "drift in no category",
      create(TargetSchema, { lastObservedAt: minutesAgo(5), highestDriftSeverity: "safe" }),
      { state: "uncategorized", severity: "safe" },
      "drift from the desired schema that no category names; the plan says what it changes",
    ],
    [
      "one finding",
      create(TargetSchema, {
        lastObservedAt: minutesAgo(5),
        highestDriftSeverity: "warning",
        driftFindingCount: 1,
      }),
      { state: "found", count: 1, severity: "warning" },
      "1 drift finding, the most severe warning",
    ],
    [
      "findings with no severity",
      create(TargetSchema, { lastObservedAt: minutesAgo(5), driftFindingCount: 3 }),
      { state: "found", count: 3, severity: "" },
      "3 drift findings",
    ],
    [
      // Counts without a read are no read.
      "findings without a read",
      create(TargetSchema, { highestDriftSeverity: "warning", driftFindingCount: 3 }),
      { state: "unread" },
      "",
    ],
  ])("%s", (_, target, reading, phrase) => {
    expect(driftReading(target)).toEqual(reading)
    expect(driftPhrase(driftReading(target))).toBe(phrase)
  })
})

describe("Drift severities", () => {
  it.each([
    ["", "safe", "safe"],
    ["safe", "info", "info"],
    ["info", "warning", "warning"],
    ["warning", "error", "error"],
    ["error", "destructive", "destructive"],
    ["destructive", "warning", "destructive"],
    ["warning", "unheard-of", "warning"],
  ])("ranks %j against %j as %j", (a, b, worse) => {
    expect(worseSeverity(a, b)).toBe(worse)
  })

  it.each([
    ["destructive", "bad"],
    ["error", "bad"],
    ["warning", "warn"],
    // A difference is never done, however safe.
    ["info", "warn"],
    ["safe", "warn"],
    ["unheard-of", "unk"],
    ["", "unk"],
  ])("reads a difference at %j as %s", (severity, tone) => {
    expect(differenceTone(severity)).toBe(tone)
  })

  it.each([
    [{ state: "unread" } as const, "off"],
    [{ state: "none" } as const, "ok"],
    [{ state: "uncategorized", severity: "safe" } as const, "warn"],
    [{ state: "found", count: 2, severity: "safe" } as const, "warn"],
    [{ state: "found", count: 2, severity: "destructive" } as const, "bad"],
    [{ state: "found", count: 2, severity: "" } as const, "unk"],
  ])("reads %j as %s", (reading, tone) => {
    expect(readingTone(reading)).toBe(tone)
  })
})
