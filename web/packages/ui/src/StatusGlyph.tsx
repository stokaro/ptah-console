import { toneStyle, type Tone } from "./tone"

export interface StatusGlyphProps {
  tone: Tone
  /** Diameter in pixels. */
  size?: 20 | 22 | 24 | 28
  /** Read to assistive technology in place of the glyph; omit when text beside it says the same. */
  label?: string
}

const sizes = {
  20: "size-5 text-[11px]",
  22: "size-[22px] text-[11px]",
  24: "size-6 text-[11px]",
  28: "size-7 text-xs",
}

/** A state as a round glyph: color and shape together. */
export function StatusGlyph({ tone, size = 22, label }: StatusGlyphProps) {
  const style = toneStyle(tone)
  return (
    <span
      className={`grid flex-none place-items-center rounded-full font-mono font-bold ${sizes[size]} ${style.bg} ${style.text}`}
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": true })}
    >
      {style.glyph}
    </span>
  )
}
