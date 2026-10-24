import { describe, expect, it } from "vitest"

import css from "./index.css?raw"

// The status tones carry text, often at 12px, on their own background, on the
// page and on a card. WCAG AA asks 4.5:1 of text that size; axe in the
// acceptance rows measures it on a real page, and this measures the tokens
// themselves, so a token change that fails it fails here first.

/** The custom properties of the block that starts at the given selector. */
function tokens(selector: string): Record<string, string> {
  const start = css.indexOf(selector)
  expect(start, `a block for ${selector}`).toBeGreaterThanOrEqual(0)
  const block = css.slice(css.indexOf("{", start) + 1, css.indexOf("}", start))
  return Object.fromEntries(
    [...block.matchAll(/--([a-z0-9]+):\s*([^;]+);/g)].map((match) => [match[1], match[2].trim()])
  )
}

function srgb(value: string): number[] {
  const hex = /^#([0-9a-f]{6})$/i.exec(value)
  if (hex) {
    return [0, 2, 4].map((at) => parseInt(hex[1].slice(at, at + 2), 16) / 255)
  }
  const oklch = /^oklch\(([\d.]+) ([\d.]+) ([\d.]+)\)$/.exec(value)
  expect(oklch, `a color this test reads: ${value}`).not.toBeNull()
  const [lightness, chroma, hue] = oklch!.slice(1).map(Number)
  const a = chroma * Math.cos((hue * Math.PI) / 180)
  const b = chroma * Math.sin((hue * Math.PI) / 180)
  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3
  return [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ].map((channel) => {
    const clamped = Math.min(Math.max(channel, 0), 1)
    return clamped <= 0.0031308 ? 12.92 * clamped : 1.055 * clamped ** (1 / 2.4) - 0.055
  })
}

function luminance(value: string) {
  const [r, g, b] = srgb(value).map((c) =>
    c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  )
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(foreground: string, background: string) {
  const [high, low] = [luminance(foreground), luminance(background)].sort((x, y) => y - x)
  return (high + 0.05) / (low + 0.05)
}

const tones = ["green", "amber", "red", "violet", "pri"]

describe.each([
  ["light", []],
  ["dark", [':root[data-theme="dark"] {']],
  ["light teal", [':root[data-accent="teal"] {']],
  ["dark teal", [':root[data-theme="dark"] {', ':root[data-theme="dark"][data-accent="teal"] {']],
  ["light graphite", [':root[data-accent="graphite"] {']],
  [
    "dark graphite",
    [':root[data-theme="dark"] {', ':root[data-theme="dark"][data-accent="graphite"] {'],
  ],
])("The %s theme's status tones", (_, selectors) => {
  it.each(tones)("read at 4.5:1 or more as text: %s", (tone) => {
    const theme = Object.assign({}, tokens(':root[data-theme="light"] {'), ...selectors.map(tokens))
    for (const background of [`${tone}bg`, "bg", "card"]) {
      expect(
        contrast(theme[tone], theme[background]),
        `--${tone} on --${background}`
      ).toBeGreaterThanOrEqual(4.5)
    }
  })
})
