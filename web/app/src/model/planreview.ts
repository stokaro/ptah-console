import { timestampDate } from "@bufbuild/protobuf/wkt"
import { shortDigest, type Tone } from "@ptah/console-ui"

import { DecisionState, type Decision } from "../gen/ptah/console/v1/decisions_pb"
import { SqlAccess, type SchemaPlan, type Statement } from "../gen/ptah/console/v1/resources_pb"
import { statementTag, type ResourceView } from "./resource"

/** How many statements carry each mark the plan gave them, the unmarked last. */
export function statementTypes(statements: Statement[]) {
  const counts = new Map<string, { tone?: Tone; count: number }>()
  let unmarked = 0
  for (const statement of statements) {
    const tag = statementTag(statement)
    if (!tag.tag) {
      unmarked++
      continue
    }
    const seen = counts.get(tag.tag) ?? { tone: tag.tone, count: 0 }
    seen.count++
    counts.set(tag.tag, seen)
  }
  const marked = [...counts.entries()]
    .map(([tag, value]) => ({ tag, tone: value.tone, count: value.count }))
    .sort((a, b) => b.count - a.count || a.tag.localeCompare(b.tag))
  return { marked, unmarked }
}

/** What the console established about the stored plan's integrity. */
export function integrity(plan: SchemaPlan): { tone: Tone; title: string; lines: string[] } {
  switch (plan.sqlAccess) {
    case SqlAccess.AVAILABLE:
      return {
        tone: "ok",
        title: "Verified",
        lines: [
          `${plan.chunks.length} ${plan.chunks.length === 1 ? "chunk" : "chunks"}, each checked by size, digest and order before any SQL was shown`,
          `Content ${shortDigest(plan.contentDigest, 8)}`,
        ],
      }
    case SqlAccess.NOT_GRANTED:
      return {
        tone: "off",
        title: "Not checked by this identity",
        lines: [
          "This identity may read the plan's manifest and not its chunks, so the SQL is not shown and its digests are not checked here.",
        ],
      }
    case SqlAccess.INTEGRITY_FAILED:
      return {
        tone: "bad",
        title: "The stored plan failed a check",
        lines: [
          plan.sqlProblem || "A chunk is missing, out of order or does not match its digest.",
          "Nothing of its SQL is shown: a partial plan is never shown as a complete one.",
        ],
      }
    case SqlAccess.UNSUPPORTED:
      return {
        tone: "unk",
        title: "Written under a contract this console does not read",
        lines: [plan.sqlProblem || `Plan contract ${plan.contractVersion}.`],
      }
    default:
      return { tone: "unk", title: "Not known", lines: [] }
  }
}

/** Statements that contain the text, and only the marked ones when asked, numbered as in the plan. */
export function filterStatements(statements: Statement[], text: string, markedOnly: boolean) {
  const wanted = text.trim().toLowerCase()
  return statements
    .map((statement, index) => ({ statement, number: index + 1 }))
    .filter(
      ({ statement }) =>
        (!markedOnly || statementTag(statement).tag !== undefined) &&
        (wanted === "" || statement.sql.toLowerCase().includes(wanted))
    )
}

/** What the page knows about a plan a link named that is no longer the current one. */
export interface Tombstone {
  name: string
  /** The plan that replaced it, if one is current. */
  current?: string
  published?: Date
  applied?: Date
  /** Approvals that named it, and what became of them. */
  approvals: { name: string; by: string; state: DecisionState }[]
}

export function tombstone(
  resource: ResourceView,
  followed: string | undefined,
  decisions: Decision[]
): Tombstone | undefined {
  if (!followed || resource.plan?.name === followed) {
    return undefined
  }
  const journal = resource.schema?.journal ?? resource.migration?.journal
  const of = (type: string) =>
    journal?.entries.filter((entry) => entry.plan === followed && entry.type === type).at(-1)?.at
  const published = of("PlanPublished")
  const applied = of("ApplyFinished")
  return {
    name: followed,
    current: resource.plan?.name,
    published: published ? timestampDate(published) : undefined,
    applied: applied ? timestampDate(applied) : undefined,
    approvals: decisions
      .filter((decision) => decision.planName === followed && decision.ownerName === resource.name)
      .map((decision) => ({
        name: decision.ref?.name ?? "",
        by: decision.decidedBy,
        state: decision.state,
      })),
  }
}

/** One check the console runs before it offers to send an approval. */
export interface ApprovalCheck {
  tone: Tone
  label: string
  note: string
}

export function approvalChecks(input: {
  current: boolean
  /** Whether the console reads the plan's contract; a migration plan has none it reads. */
  contractSupported?: boolean
  allowed: "allowed" | "read-only" | "denied" | "offline"
  namespace: string
}): ApprovalCheck[] {
  const checks: (ApprovalCheck | undefined)[] = [
    input.current
      ? { tone: "ok", label: "The plan you read is the current plan", note: "" }
      : {
          tone: "bad",
          label: "The plan you read is no longer current",
          note: "Read the current plan before approving anything.",
        },
    input.contractSupported === undefined
      ? undefined
      : input.contractSupported
        ? { tone: "ok", label: "Its contract is one this console reads", note: "" }
        : {
            tone: "bad",
            label: "Its contract is not one this console reads",
            note: "Approve it with a console built for that contract.",
          },
    input.allowed === "allowed"
      ? { tone: "ok", label: `This identity may create approvals in ${input.namespace}`, note: "" }
      : input.allowed === "read-only"
        ? { tone: "off", label: "This console sends no decision", note: "It runs read-only." }
        : input.allowed === "offline"
          ? {
              tone: "warn",
              label: "The cluster is out of reach",
              note: "Decisions wait until it is back.",
            }
          : {
              tone: "bad",
              label: `This identity may not create approvals in ${input.namespace}`,
              note: "Share the link with an approver.",
            },
  ]
  return checks.filter((check): check is ApprovalCheck => check !== undefined)
}
