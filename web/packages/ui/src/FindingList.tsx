import { StatusGlyph } from "./StatusGlyph"
import type { Tone } from "./tone"

export interface Finding {
  tone: Tone
  text: string
}

/** What a plan changes, one line per finding, each with its state. */
export function FindingList({
  findings,
  size = "md",
}: {
  findings: Finding[]
  size?: "sm" | "md"
}) {
  return (
    <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
      {findings.map((finding) => (
        <li
          key={finding.text}
          className={`grid items-center gap-2.5 ${size === "sm" ? "grid-cols-[22px_minmax(0,1fr)] text-[13px]" : "grid-cols-[24px_minmax(0,1fr)] text-sm"}`}
        >
          <StatusGlyph tone={finding.tone} size={size === "sm" ? 20 : 24} />
          {finding.text}
        </li>
      ))}
    </ul>
  )
}
