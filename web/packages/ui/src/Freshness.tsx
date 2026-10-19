import { StatusGlyph } from "./StatusGlyph"
import type { Tone } from "./tone"

export type FreshnessState = "connecting" | "live" | "lost" | "denied" | "ended"

export interface FreshnessProps {
  state: FreshnessState
  /** When the data was last known to be current. */
  at?: Date
  now: Date
}

function ago(at: Date, now: Date) {
  const seconds = Math.max(0, Math.round((now.getTime() - at.getTime()) / 1000))
  return seconds < 60 ? `${seconds} s ago` : `${Math.floor(seconds / 60)} min ago`
}

const tones: Record<FreshnessState, Tone> = {
  connecting: "off",
  live: "run",
  lost: "warn",
  denied: "off",
  ended: "bad",
}

/** How current the page is: live, and since when, or since when not. */
export function Freshness({ state, at, now }: FreshnessProps) {
  const words = {
    connecting: "Connecting…",
    live: at ? `Live · current ${ago(at, now)}` : "Live",
    lost: at ? `Updates stopped · last current ${ago(at, now)}` : "Updates stopped",
    denied: "Not live: this identity may not watch; pages refresh when you act",
    ended: "Session ended",
  }[state]
  return (
    <span role="status" className="inline-flex items-center gap-1.5 text-xs text-fg2">
      <StatusGlyph tone={tones[state]} size={20} />
      {words}
    </span>
  )
}
