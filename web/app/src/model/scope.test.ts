import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { Access, Answer, NamespaceScopeSchema } from "../gen/ptah/console/v1/resources_pb"
import {
  accessLabel,
  choiceKey,
  choiceOf,
  inScope,
  matching,
  namespacesRead,
  parseChoice,
  serializeChoice,
} from "./scope"

describe("A stored choice", () => {
  it.each([
    ['{"all":true}', { all: true }],
    [
      '{"all":false,"namespaces":["billing","application"]}',
      { all: false, namespaces: ["billing", "application"] },
    ],
    ['{"all":false,"namespaces":["billing","billing"]}', { all: false, namespaces: ["billing"] }],
  ])("reads %s", (raw, want) => {
    expect(parseChoice(raw)).toEqual(want)
  })

  it.each([
    ["nothing stored", null],
    ["not JSON", "billing"],
    ["a list", '["billing"]'],
    ["no all", '{"namespaces":["billing"]}'],
    ["all that is not a boolean", '{"all":"yes"}'],
    ["no namespaces", '{"all":false,"namespaces":[]}'],
    ["a name no namespace has", '{"all":false,"namespaces":["Billing"]}'],
    ["a name that is not a string", '{"all":false,"namespaces":[7]}'],
    ["null", "null"],
  ])("is no choice when it is %s", (_name, raw) => {
    expect(parseChoice(raw)).toBeUndefined()
  })

  it("reads back what was stored", () => {
    for (const choice of [
      { all: true } as const,
      { all: false as const, namespaces: ["billing"] },
    ]) {
      expect(parseChoice(serializeChoice(choice))).toEqual(choice)
    }
  })

  it("is kept apart for each cluster and each identity", () => {
    const keys = new Set([
      choiceKey("kind-ptah", "alice"),
      choiceKey("kind-ptah", "bob"),
      choiceKey("prod", "alice"),
    ])
    expect(keys.size).toBe(3)
  })
})

describe("A selection", () => {
  const readable = ["application", "billing", "reports"]

  it("of every readable namespace is all of them, so one granted later joins", () => {
    expect(choiceOf(["reports", "billing", "application"], readable)).toEqual({ all: true })
  })

  it("of some names them in the order the server offered them", () => {
    expect(choiceOf(["reports", "application"], readable)).toEqual({
      all: false,
      namespaces: ["application", "reports"],
    })
  })

  it("leaves out a namespace that is not readable", () => {
    expect(choiceOf(["application", "payroll"], readable)).toEqual({
      all: false,
      namespaces: ["application"],
    })
  })

  it("of none is no choice", () => {
    expect(choiceOf([], readable)).toBeUndefined()
    expect(choiceOf(["payroll"], readable)).toBeUndefined()
  })
})

describe("What a choice selects", () => {
  const readable = ["application", "billing", "reports"]

  it("is every readable namespace without a choice, or with all", () => {
    for (const choice of [undefined, { all: true } as const]) {
      expect(inScope(readable, choice)).toEqual({
        namespaces: readable,
        narrowed: false,
        lapsed: false,
      })
    }
  })

  it("is the chosen ones that are readable now", () => {
    expect(
      inScope(readable, { all: false, namespaces: ["reports", "payroll", "application"] })
    ).toEqual({ namespaces: ["application", "reports"], narrowed: true, lapsed: false })
  })

  it("is every readable one, said to have lapsed, when none chosen is readable now", () => {
    expect(inScope(readable, { all: false, namespaces: ["payroll"] })).toEqual({
      namespaces: readable,
      narrowed: false,
      lapsed: true,
    })
  })

  it("has not lapsed when nothing is readable at all", () => {
    expect(inScope([], { all: false, namespaces: ["payroll"] }).lapsed).toBe(false)
  })

  it("is not narrowed when the chosen ones are all that is readable", () => {
    expect(inScope(["billing"], { all: false, namespaces: ["billing"] }).narrowed).toBe(false)
  })
})

describe("A search", () => {
  const names = ["application", "billing-eu", "Billing-Us"]

  it("matches part of a name, ignoring case and the spaces around it", () => {
    expect(matching(names, "  BILL ")).toEqual(["billing-eu", "Billing-Us"])
  })

  it("of nothing matches every name", () => {
    expect(matching(names, " ")).toEqual(names)
  })

  it("can match nothing", () => {
    expect(matching(names, "payroll")).toEqual([])
  })
})

describe("A namespace's access", () => {
  const scope = (access: Access, approve: Answer = Answer.DENIED) =>
    create(NamespaceScopeSchema, {
      name: "application",
      access,
      capabilities: { listSchemas: Answer.ALLOWED, approveSchemaPlans: approve },
    })

  it.each([
    ["a reader who may approve", scope(Access.OK, Answer.ALLOWED), "may read and decide"],
    ["a reader who may not", scope(Access.OK), "may read"],
    ["a reader whose approval is unknown", scope(Access.OK, Answer.UNKNOWN), "may read"],
    ["one refused", scope(Access.FORBIDDEN), "may not read"],
    ["one the server could not judge", scope(Access.ERROR), "access unknown"],
  ])("reads for %s as %j", (_name, given, want) => {
    expect(accessLabel(given)).toBe(want)
  })
})

describe("How many namespaces a page reads", () => {
  it.each([
    [1, 1, "1 namespace"],
    [3, 3, "3 namespaces"],
    [2, 5, "2 of the 5 namespaces"],
  ])("%i of %i reads as %j", (read, readable, want) => {
    expect(namespacesRead(read, readable)).toBe(want)
  })
})
