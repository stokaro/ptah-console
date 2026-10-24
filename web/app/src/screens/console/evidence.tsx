import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt"
import { Segmented, StatusGlyph } from "@ptah/console-ui"
import { useState } from "react"

import { Evidence, type GetDiagnosticsResponse } from "../../gen/ptah/console/v1/diagnostics_pb"
import { ago } from "../../model/connection"
import { relatedResources } from "../../model/diagnosis"
import { goneWord } from "../../model/execution"
import type { ResourceView } from "../../model/resource"

function date(timestamp: Timestamp | undefined) {
  return timestamp ? timestampDate(timestamp) : undefined
}

type EventFilter = "all" | "Normal" | "Warning"

/**
 * The Kubernetes Events about the resource, as the API server still keeps
 * them, newest first. With filters, a reader narrows them to warnings.
 */
export function ResourceEvents({
  response,
  now,
  filters = false,
}: {
  response?: GetDiagnosticsResponse
  now: Date
  /** Offer to narrow the Events by type. */
  filters?: boolean
}) {
  const [filter, setFilter] = useState<EventFilter>("all")
  const list = response?.events
  if (!list) {
    return <span className="text-sm text-fg2">Not read yet.</span>
  }
  if (list.evidence !== Evidence.SHOWN) {
    return (
      <span className="text-sm text-fg2">
        Events: {goneWord(list.evidence)}
        {list.problem ? ` (${list.problem})` : ""}
      </span>
    )
  }
  if (list.events.length === 0) {
    return <span className="text-sm text-fg2">No Event is kept about this resource.</span>
  }
  const shown = list.events.filter((event) => filter === "all" || event.type === filter)
  const oldest = list.events
    .map((event) => date(event.firstSeen ?? event.lastSeen))
    .filter((seen): seen is Date => seen !== undefined)
    .sort((a, b) => a.getTime() - b.getTime())[0]
  return (
    <>
      {filters ? (
        <Segmented
          label="Show Events"
          value={filter}
          onChange={setFilter}
          options={[
            { value: "all", label: "All", count: list.events.length },
            {
              value: "Normal",
              label: "Normal",
              count: list.events.filter((event) => event.type !== "Warning").length,
            },
            {
              value: "Warning",
              label: "Warning",
              count: list.events.filter((event) => event.type === "Warning").length,
            },
          ]}
        />
      ) : null}
      {shown.length === 0 ? (
        <span className="text-sm text-fg2">No Event of this type is kept.</span>
      ) : (
        <ul aria-label="Events" className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
          {shown.map((event, index) => {
            const seen = date(event.lastSeen ?? event.firstSeen)
            return (
              <li
                key={`${event.reason}-${index}`}
                className="grid grid-cols-[20px_minmax(0,1fr)_auto] gap-2"
              >
                <StatusGlyph tone={event.type === "Warning" ? "warn" : "off"} size={20} />
                <span className="flex min-w-0 flex-col">
                  <b className="font-semibold">
                    {event.reason}
                    {event.count > 1 ? ` ×${event.count}` : ""}
                  </b>
                  <span className="text-fg2">{event.message}</span>
                </span>
                <span className="text-xs whitespace-nowrap text-mute">
                  {seen ? ago(seen, now) : "–"}
                </span>
              </li>
            )
          })}
        </ul>
      )}
      <span className="text-xs text-mute">
        The API server keeps Events for an hour by default
        {oldest ? `; the oldest kept here is from ${oldest.toLocaleTimeString()}` : ""}.
      </span>
    </>
  )
}

/** What the resource reads and what it produced, as its status names them. */
export function RelatedResources({ resource }: { resource: ResourceView }) {
  return (
    <ul className="m-0 flex list-none flex-col gap-1.5 p-0 text-[13px]">
      {relatedResources(resource).map((item) => (
        <li key={`${item.kind}/${item.name}/${item.note}`} className="flex flex-col">
          <span>
            <span className="text-mute">{item.kind}</span>{" "}
            <span className="font-mono break-all">{item.name}</span>
          </span>
          {item.note ? <span className="text-xs text-mute">{item.note}</span> : null}
        </li>
      ))}
    </ul>
  )
}
