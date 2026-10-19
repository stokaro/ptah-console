import { describe, expect, it } from "vitest"

import { sentence } from "./resource"

describe("sentence", () => {
  it.each([
    ["a fresh read shows no differences", "A fresh read shows no differences."],
    ["Waits for an approval.", "Waits for an approval."],
    ["  ", ""],
    [undefined, ""],
  ])("%s", (detail, want) => {
    expect(sentence(detail)).toBe(want)
  })
})
