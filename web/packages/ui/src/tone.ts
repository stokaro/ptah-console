/**
 * The six states the console draws. A state is always a color and a glyph
 * together, never color alone.
 */
export type Tone = "ok" | "warn" | "bad" | "unk" | "run" | "off"

export interface ToneStyle {
  /** Classes for the glyph's text color. */
  text: string
  /** Classes for the glyph's background. */
  bg: string
  /** The glyph itself. */
  glyph: string
  /** What the glyph means, for assistive technology. */
  label: string
}

const styles: Record<Tone, ToneStyle> = {
  ok: { text: "text-green", bg: "bg-greenbg", glyph: "✓", label: "done" },
  warn: { text: "text-amber", bg: "bg-amberbg", glyph: "!", label: "needs attention" },
  bad: { text: "text-red", bg: "bg-redbg", glyph: "×", label: "failed" },
  unk: { text: "text-violet", bg: "bg-violetbg", glyph: "?", label: "unknown" },
  run: { text: "text-run", bg: "bg-runbg", glyph: "●", label: "in progress" },
  off: { text: "text-mute", bg: "bg-greybg", glyph: "–", label: "not applicable" },
}

export function toneStyle(tone: Tone): ToneStyle {
  return styles[tone]
}
