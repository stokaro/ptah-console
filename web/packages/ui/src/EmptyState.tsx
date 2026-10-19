import type { ReactNode } from "react"

import type { Tone } from "./tone"
import { toneStyle } from "./tone"

/** A page or list with nothing to show, and why. */
export function EmptyState({
  tone = "ok",
  title,
  children,
}: {
  tone?: Tone
  title: string
  children?: ReactNode
}) {
  const style = toneStyle(tone)
  return (
    <section className="flex flex-col items-center gap-2 rounded-[18px] border border-line bg-card p-9 text-center shadow-card">
      <span
        aria-hidden="true"
        className={`grid size-[52px] place-items-center rounded-2xl font-mono text-[22px] font-bold ${style.bg} ${style.text}`}
      >
        {style.glyph}
      </span>
      <h2 className="m-0 font-display text-lg font-semibold">{title}</h2>
      {children ? <div className="text-[13.5px] text-fg2">{children}</div> : null}
    </section>
  )
}
