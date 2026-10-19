import { StatusGlyph } from "./StatusGlyph"
import type { Tone } from "./tone"

export interface Risk {
  tone: Tone
  text: string
}

/** What a plan changes, one line per finding, each with its state. */
export function RiskList({ risks, size = "md" }: { risks: Risk[]; size?: "sm" | "md" }) {
  return (
    <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
      {risks.map((risk) => (
        <li
          key={risk.text}
          className={`grid items-center gap-2.5 ${size === "sm" ? "grid-cols-[22px_minmax(0,1fr)] text-[13px]" : "grid-cols-[24px_minmax(0,1fr)] text-sm"}`}
        >
          <StatusGlyph tone={risk.tone} size={size === "sm" ? 20 : 24} />
          {risk.text}
        </li>
      ))}
    </ul>
  )
}
