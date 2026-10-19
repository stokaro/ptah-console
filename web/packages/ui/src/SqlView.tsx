import type { Tone } from "./tone"

export interface SqlLine {
  /** The statement's position in the plan, from 1. */
  number: number
  sql: string
  /** A short marker from the plan, such as "destructive". */
  tag?: string
  tagTone?: Tone
}

const tagStyles: Record<Tone, string> = {
  ok: "bg-greenbg text-green",
  warn: "bg-amberbg text-amber",
  bad: "bg-redbg text-red",
  unk: "bg-violetbg text-violet",
  run: "bg-pribg text-pri",
  off: "bg-greybg text-mute",
}

/**
 * A plan's statements as plain text, numbered, with the plan's own markers.
 * The text is rendered as text: nothing in it is ever interpreted.
 */
export function SqlView({ lines, label = "SQL statements" }: { lines: SqlLine[]; label?: string }) {
  return (
    <ol
      aria-label={label}
      className="m-0 list-none overflow-hidden rounded-xl border border-line2 p-0"
    >
      {lines.map((line) => (
        <li
          key={line.number}
          className="grid grid-cols-[28px_minmax(0,1fr)_auto] items-start gap-2 border-b border-line2 px-2.5 py-2 font-mono text-[12.5px] leading-[1.55] last:border-b-0"
        >
          <span className="text-mute">{line.number}</span>
          <span className="break-words whitespace-pre-wrap">{line.sql}</span>
          {line.tag ? (
            <span
              className={`rounded-md px-[7px] py-px font-sans text-[11px] font-semibold whitespace-nowrap ${tagStyles[line.tagTone ?? "off"]}`}
            >
              {line.tag}
            </span>
          ) : null}
        </li>
      ))}
    </ol>
  )
}
