import { shortDigest, StatusGlyph, StepRail, EmptyState } from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"
import { useState } from "react"

import { useResources, useSession } from "../api/resources"
import { say, steps, type ResourceView } from "../model/resource"
import { Hero, Loading, ReadError, UnreadableNamespaces, useNow, where } from "./common"

function matches(resource: ResourceView, query: string) {
  const q = query.trim().toLowerCase()
  return (
    q === "" ||
    resource.name.toLowerCase().includes(q) ||
    resource.namespace.toLowerCase().includes(q) ||
    resource.source.toLowerCase().includes(q) ||
    resource.sourceDigest.toLowerCase().includes(q)
  )
}

export function Releases() {
  const { resources, scope, loading, error } = useResources()
  const session = useSession()
  const now = useNow()
  const [query, setQuery] = useState("")
  const shown = resources
    .filter((resource) => matches(resource, query))
    .sort((a, b) => a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name))

  return (
    <>
      <Hero
        eyebrow="Developer"
        count={resources.length}
        label={resources.length === 1 ? "release to follow" : "releases to follow"}
        sub="Did my release reach the database"
        capability="get ptahschemas, ptahmigrations"
      />
      <UnreadableNamespaces scopes={scope.scopes} />
      <label className="flex h-[46px] items-center gap-2 rounded-[14px] border border-line bg-card px-3.5 text-mute">
        <span aria-hidden="true">⌕</span>
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Resource, namespace or digest"
          aria-label="Find a release"
          className="min-w-0 flex-auto border-0 bg-transparent text-[15px] text-fg outline-0"
        />
      </label>
      {loading ? (
        <Loading what="the releases" />
      ) : error ? (
        <ReadError error={error} what="the releases" />
      ) : shown.length === 0 ? (
        <EmptyState tone="off" title={query ? "Nothing matches" : "No schema or migration here"}>
          {query
            ? "Try a shorter name or digest."
            : "Nothing in the namespaces this identity may read."}
        </EmptyState>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-3 p-0" aria-label="Releases">
          {shown.map((resource) => {
            const sentence = say(resource, now)
            return (
              <li
                key={resource.ref.uid}
                className="flex animate-[pt-rise_.4s_both] flex-col gap-3 rounded-[18px] border border-line bg-card p-[18px] shadow-card transition-[border-color,transform] duration-200 hover:-translate-y-px hover:border-[color-mix(in_oklab,var(--pri)_40%,var(--line))]"
              >
                <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3">
                  <span className="flex flex-col gap-[3px]">
                    <Link
                      to="/releases/$namespace/$kind/$name"
                      params={{
                        namespace: resource.namespace,
                        kind: resource.kind,
                        name: resource.name,
                      }}
                      className="font-mono text-base font-semibold text-fg"
                    >
                      {resource.name}{" "}
                      <span className="font-normal text-mute">
                        {resource.sourceDigest ? shortDigest(resource.sourceDigest) : ""}
                      </span>
                    </Link>
                    <span className="text-xs text-mute">
                      {where(session.data?.context, resource.namespace)} ·{" "}
                      {resource.kind === "schema" ? "Schema" : "Migration"}
                    </span>
                  </span>
                  <span aria-hidden="true" className="font-mono text-base font-semibold text-mute">
                    ›
                  </span>
                </div>
                <StepRail steps={steps(resource)} label={`Progress of ${resource.name}`} />
                <Sentence tone={sentence.tone} text={sentence.text} />
              </li>
            )
          })}
        </ul>
      )}
    </>
  )
}

const sentenceBg = {
  ok: "bg-[color-mix(in_oklab,var(--greenbg)_60%,var(--card))]",
  warn: "bg-[color-mix(in_oklab,var(--amberbg)_60%,var(--card))]",
  bad: "bg-[color-mix(in_oklab,var(--redbg)_60%,var(--card))]",
  unk: "bg-[color-mix(in_oklab,var(--violetbg)_60%,var(--card))]",
  run: "bg-[color-mix(in_oklab,var(--pribg)_60%,var(--card))]",
  off: "bg-[color-mix(in_oklab,var(--greybg)_60%,var(--card))]",
} as const

/** One sentence on where a resource stands, with its state. */
export function Sentence({
  tone,
  text,
  large = false,
}: {
  tone: keyof typeof sentenceBg
  text: string
  large?: boolean
}) {
  return (
    <div
      className={`grid grid-cols-[26px_minmax(0,1fr)] gap-2.5 rounded-xl p-3 ${sentenceBg[tone]}`}
    >
      <StatusGlyph tone={tone} size={24} />
      <span className={`leading-normal ${large ? "text-[15px]" : "text-sm"}`}>{text}</span>
    </div>
  )
}
