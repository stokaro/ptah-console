import { describe, expect, it } from "vitest"

import { toneStyle, type Tone } from "./tone"

const tones: Tone[] = ["ok", "warn", "bad", "unk", "run", "off"]

describe("toneStyle", () => {
  // A viewer may change the accent, the pri tokens; a state drawn with them
  // would change its color with it.
  it("draws no state in the accent's colors", () => {
    for (const tone of tones) {
      const { text, bg } = toneStyle(tone)
      expect(`${text} ${bg}`).not.toMatch(/\bpri/)
    }
  })

  it("gives every state its own glyph", () => {
    expect(new Set(tones.map((tone) => toneStyle(tone).glyph)).size).toBe(tones.length)
  })
})
