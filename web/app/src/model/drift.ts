import type { Tone } from "@ptah/console-ui"

import type { Target } from "../gen/ptah/console/v1/resources_pb"

/**
 * What the operator's last read of the database found, from the three fields
 * it publishes. A report can find drift it lists under no category: the
 * severity is set then while the count is zero, so a zero count alone does
 * not mean the database matches.
 */
export type DriftReading =
  | { state: "unread" }
  | { state: "none" }
  | { state: "uncategorized"; severity: string }
  | { state: "found"; count: number; severity: string }

export function driftReading(target: Target | undefined): DriftReading {
  if (!target?.lastObservedAt) {
    return { state: "unread" }
  }
  if (target.driftFindingCount > 0) {
    return {
      state: "found",
      count: target.driftFindingCount,
      severity: target.highestDriftSeverity,
    }
  }
  if (target.highestDriftSeverity) {
    return { state: "uncategorized", severity: target.highestDriftSeverity }
  }
  return { state: "none" }
}

/**
 * What a read found, as a phrase to follow "The last read found" or "Read
 * 5 min ago:". An unread database has no phrase. The count is of findings,
 * not of objects: one object can be a finding in two categories, as an index
 * removed with the uniqueness it enforced is.
 */
export function driftPhrase(reading: DriftReading): string {
  switch (reading.state) {
    case "unread":
      return ""
    case "none":
      return "no drift from the desired schema"
    case "uncategorized":
      return "drift from the desired schema that no category names; the plan says what it changes"
    case "found":
      return `${reading.count} drift ${reading.count === 1 ? "finding" : "findings"}${reading.severity ? `, the most severe ${reading.severity}` : ""}`
  }
}

// The status admits these five, from the least to the most severe.
const severityOrder = ["safe", "info", "warning", "error", "destructive"]

/** The more severe of two severities; an unknown one ranks below safe. */
export function worseSeverity(a: string, b: string): string {
  return severityOrder.indexOf(b) > severityOrder.indexOf(a) ? b : a
}

/**
 * The tone of a difference at a severity. A difference is never done, so even
 * a safe one reads as needing attention; the severity beside it says how much.
 * A severity the console does not know reads unknown.
 */
export function differenceTone(severity: string): Tone {
  switch (severity) {
    case "destructive":
    case "error":
      return "bad"
    case "warning":
    case "info":
    case "safe":
      return "warn"
    default:
      return "unk"
  }
}

/** The tone of a reading: done only when the read found no drift at all. */
export function readingTone(reading: DriftReading): Tone {
  switch (reading.state) {
    case "unread":
      return "off"
    case "none":
      return "ok"
    case "uncategorized":
      return "warn"
    case "found":
      return differenceTone(reading.severity)
  }
}
