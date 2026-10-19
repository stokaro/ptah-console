import { EmptyState, StatusGlyph } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { canAnywhere, useResources, useSession } from "../api/resources"
import { attentionTitle, attentionTone, incident } from "../model/resource"
import { Hero, Loading, ReadError, UnreadableNamespaces, where } from "./common"

const borders = {
  ok: "border-l-green",
  warn: "border-l-amber",
  bad: "border-l-red",
  unk: "border-l-violet",
  run: "border-l-pri",
  off: "border-l-mute",
} as const

export function Incidents() {
  const { resources, scope, loading, error } = useResources()
  const session = useSession()
  const open = resources
    .map((resource) => ({ resource, attention: incident(resource) }))
    .filter((item) => item.attention)
  const capability = [
    canAnywhere(scope.scopes, "readEvents") ? "list events" : "",
    canAnywhere(scope.scopes, "readLogs") ? "get pods/log" : "",
    canAnywhere(scope.scopes, "acknowledgeRuns") ? "create ptahmigrationrunacknowledgments" : "",
  ]
    .filter(Boolean)
    .join(", ")

  return (
    <>
      <Hero
        eyebrow="On call"
        count={open.length}
        label={open.length === 1 ? "thing needs a person" : "things need a person"}
        sub="What is broken and the next supported step"
        capability={capability || "get ptahschemas, ptahmigrations"}
      />
      <UnreadableNamespaces scopes={scope.scopes} />
      {loading ? (
        <Loading what="what needs a person" />
      ) : error ? (
        <ReadError error={error} what="the resources" />
      ) : open.length === 0 ? (
        <EmptyState title="Nothing is broken">
          Every schema and migration here is where the operator wants it.
        </EmptyState>
      ) : (
        <ul
          className="m-0 flex list-none flex-col gap-3 p-0"
          aria-label="Things that need a person"
        >
          {open.map(({ resource, attention }) => {
            const tone = attentionTone(attention!.kind)
            return (
              <li
                key={resource.ref.uid}
                className={`flex animate-[pt-rise_.4s_both] flex-col gap-3 rounded-[18px] border border-l-4 border-line bg-card p-[18px] shadow-card ${borders[tone]}`}
              >
                <div className="grid grid-cols-[24px_minmax(0,1fr)_auto] items-center gap-2.5">
                  <StatusGlyph tone={tone} size={24} />
                  <span className="flex flex-col gap-0.5">
                    <Link
                      to="/incidents/$namespace/$kind/$name"
                      params={{
                        namespace: resource.namespace,
                        kind: resource.kind,
                        name: resource.name,
                      }}
                      className="text-base font-semibold text-fg"
                    >
                      {attentionTitle(attention!.kind)}: {resource.name}
                    </Link>
                    <span className="text-xs text-mute">
                      {where(session.data?.context, resource.namespace)}
                    </span>
                  </span>
                  <span aria-hidden="true" className="font-mono text-base font-semibold text-mute">
                    ›
                  </span>
                </div>
                <span className="text-sm leading-normal text-fg2">{attention!.reason}</span>
              </li>
            )
          })}
        </ul>
      )}
      <span className="text-[12.5px] text-mute">
        Retry, rollback and force apply are not offered: they are not part of the operator contract.
      </span>
    </>
  )
}
