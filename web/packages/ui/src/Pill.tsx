import type { ReactNode } from "react"

import type { Tone } from "./tone"

const tones: Record<Tone | "plain", string> = {
  plain: "bg-greybg text-fg2",
  ok: "bg-greenbg text-green",
  warn: "bg-amberbg text-amber",
  bad: "bg-redbg text-red",
  unk: "bg-violetbg text-violet",
  run: "bg-runbg text-run",
  off: "bg-greybg text-mute",
}

/** A short label beside a heading or a name, such as "Read-only". */
export function Pill({ tone = "plain", children }: { tone?: Tone | "plain"; children: ReactNode }) {
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium whitespace-nowrap ${tones[tone]}`}
    >
      {children}
    </span>
  )
}
