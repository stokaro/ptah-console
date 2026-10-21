import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import {
  Access,
  AttentionKind,
  AttentionSchema,
  NamespaceScopeSchema,
  StageState,
} from "../gen/ptah/console/v1/resources_pb"
import { minutesAgo, syncedMigration, waitingSchema } from "../test/console"
import {
  leadingAttention,
  namespaceHealth,
  needsAttention,
  recentActivity,
  stateCounts,
} from "./overview"
import { fromMigration, fromSchema, type ResourceView } from "./resource"

function attention(kind: AttentionKind, minutes?: number) {
  return create(AttentionSchema, {
    kind,
    reason: `reason ${kind}`,
    since: minutes === undefined ? undefined : minutesAgo(minutes),
  })
}

function schemaWith(name: string, namespace: string, ...items: ReturnType<typeof attention>[]) {
  return fromSchema(
    waitingSchema((schema) => {
      schema.ref!.name = name
      schema.ref!.namespace = namespace
      schema.ref!.uid = `uid-${namespace}-${name}`
      schema.attention = items
    })
  )
}

const synced = fromMigration(syncedMigration())

describe("leadingAttention", () => {
  it("is the first attention the server lists", () => {
    const failed = schemaWith(
      "a",
      "application",
      attention(AttentionKind.FAILED, 5),
      attention(AttentionKind.WAITING_FOR_APPROVAL, 9)
    )
    expect(leadingAttention(failed)?.kind).toBe(AttentionKind.FAILED)
  })

  it("is none for a suspended resource, whatever else it waits on", () => {
    const suspended = schemaWith(
      "a",
      "application",
      attention(AttentionKind.WAITING_FOR_APPROVAL, 9),
      attention(AttentionKind.SUSPENDED)
    )
    expect(leadingAttention(suspended)).toBeUndefined()
  })

  it("is none for a resource with nothing to look at", () => {
    expect(leadingAttention(synced)).toBeUndefined()
  })
})

describe("needsAttention", () => {
  it("puts an unknown outcome before a failure, a failure before an approval, the longest wait first", () => {
    const resources: ResourceView[] = [
      schemaWith("newer-approval", "application", attention(AttentionKind.WAITING_FOR_APPROVAL, 2)),
      schemaWith("drifted", "application", attention(AttentionKind.DRIFT, 50)),
      schemaWith(
        "older-approval",
        "application",
        attention(AttentionKind.WAITING_FOR_APPROVAL, 40)
      ),
      schemaWith("undated-approval", "application", attention(AttentionKind.WAITING_FOR_APPROVAL)),
      schemaWith("blocked", "application", attention(AttentionKind.BLOCKED, 1)),
      schemaWith("unresolved", "application", attention(AttentionKind.UNRESOLVED_RUN, 1)),
      synced,
    ]

    expect(needsAttention(resources).map((row) => [row.resource.name, row.group])).toEqual([
      ["unresolved", "unknown"],
      ["blocked", "failing"],
      ["older-approval", "approval"],
      ["newer-approval", "approval"],
      ["undated-approval", "approval"],
      ["drifted", "drift"],
    ])
  })
})

describe("namespaceHealth", () => {
  const scopes = [
    create(NamespaceScopeSchema, { name: "application", access: Access.OK }),
    create(NamespaceScopeSchema, { name: "billing", access: Access.OK }),
    create(NamespaceScopeSchema, { name: "payroll", access: Access.FORBIDDEN }),
  ]

  it("counts each namespace's states and keeps an unreadable one as such", () => {
    const resources = [
      schemaWith("a", "billing", attention(AttentionKind.FAILED, 3)),
      schemaWith("b", "billing", attention(AttentionKind.WAITING_FOR_APPROVAL, 3)),
      synced,
    ]

    const rows = namespaceHealth(resources, scopes)

    expect(
      rows.map((row) => [row.namespace, row.readable, row.counts.attention, row.total])
    ).toEqual([
      ["billing", true, 2, 2],
      ["application", true, 0, 1],
      ["payroll", false, 0, 0],
    ])
    expect(rows[1].counts.insync).toBe(1)
  })

  it("keeps a namespace with nothing in it, at zero", () => {
    const rows = namespaceHealth([], scopes.slice(0, 1))
    expect(rows).toEqual([
      {
        namespace: "application",
        readable: true,
        counts: { attention: 0, progress: 0, insync: 0, suspended: 0, none: 0 },
        total: 0,
      },
    ])
  })
})

describe("stateCounts", () => {
  it("counts every resource once", () => {
    const applying = fromSchema(
      waitingSchema((schema) => {
        schema.attention = []
        schema.stages[2].state = StageState.DONE
        schema.stages[3].state = StageState.ACTIVE
      })
    )
    expect(
      stateCounts([synced, applying, schemaWith("a", "x", attention(AttentionKind.DRIFT))])
    ).toEqual({
      counts: { attention: 1, progress: 1, insync: 1, suspended: 0, none: 0 },
      total: 3,
    })
  })
})

describe("recentActivity", () => {
  it("lists the newest transitions first, and leaves an undated one out", () => {
    const schema = fromSchema(
      waitingSchema((s) => {
        s.conditions[0].lastTransition = minutesAgo(30)
        s.conditions[1].lastTransition = undefined
      })
    )

    const activity = recentActivity([schema, synced], 5)

    expect(activity.map((a) => [a.resource.name, a.condition.type])).toEqual([
      ["storefront", "PlanReady"],
      ["billing", "InSync"],
    ])
  })

  it("stops at the limit", () => {
    expect(recentActivity([fromSchema(waitingSchema()), synced], 2)).toHaveLength(2)
  })
})
