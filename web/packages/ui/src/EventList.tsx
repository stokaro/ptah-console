import { StatusGlyph } from "./StatusGlyph"
import type { Tone } from "./tone"

export interface TimelineEvent {
  key: string
  /** When, as already formatted for the reader. */
  time: string
  tone: Tone
  title: string
  detail?: string
}

/** What happened, in order, each event with its time and state. */
export function EventList({
  events,
  label = "Events",
}: {
  events: TimelineEvent[]
  label?: string
}) {
  return (
    <ol aria-label={label} className="m-0 flex list-none flex-col p-0">
      {events.map((event) => (
        <li
          key={event.key}
          className="grid grid-cols-[64px_24px_minmax(0,1fr)] items-start gap-2.5 border-t border-line2 py-2"
        >
          <span className="pt-[3px] font-mono text-xs text-mute">{event.time}</span>
          <StatusGlyph tone={event.tone} />
          <span className="flex min-w-0 flex-col gap-0.5">
            <span className="text-[13.5px] font-semibold">{event.title}</span>
            {event.detail ? (
              <span className="text-[12.5px] break-words text-fg2">{event.detail}</span>
            ) : null}
          </span>
        </li>
      ))}
    </ol>
  )
}
