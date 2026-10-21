import {
  Button,
  EmptyState,
  Eyebrow,
  Segmented,
  shortDigest,
  StatusGlyph,
  StepDots,
} from "@ptah/console-ui"
import { Link } from "@tanstack/react-router"

import { useResources } from "../../api/resources"
import {
  pageSizes,
  paginate,
  resourceStates,
  select,
  sortOrders,
  stateLabel,
  updatedAt,
  type CatalogQuery,
} from "../../model/catalog"
import { say, steps, type ResourceView } from "../../model/resource"
import { Loading, ReadError, TruncatedNotice, UnreadableNamespaces, useNow } from "../common"
import { resourceLink } from "./links"
import type { ResourcesSearch } from "./search"

export interface ResourcesProps {
  search: ResourcesSearch
  /** Moves the list to another state; the page sets what it changes. */
  onSearch: (search: ResourcesSearch) => void
}

export function Resources({ search, onSearch }: ResourcesProps) {
  const { resources, scope, truncated, loading, error } = useResources()
  const now = useNow()
  const query: CatalogQuery = {
    q: search.q ?? "",
    kind: search.kind ?? "all",
    state: search.state ?? "all",
    sort: search.sort ?? "attention",
  }
  const size = search.size ?? pageSizes[0]
  const { rows, kindCounts, stateCounts } = select(resources, query)
  const page = paginate(rows, search.page ?? 1, size)
  const filtered = query.q !== "" || query.kind !== "all" || query.state !== "all"
  // Any change of filter starts again from the first page.
  const change = (next: ResourcesSearch) => onSearch({ ...search, page: undefined, ...next })

  return (
    <>
      <section className="flex flex-col gap-1.5">
        <Eyebrow>Workspace</Eyebrow>
        <h1 className="m-0 flex flex-wrap items-baseline gap-3 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
          Resources
          <span className="font-mono text-base font-medium text-mute">{resources.length}</span>
        </h1>
        <span className="text-[13.5px] text-fg2">
          Every schema and migration in {scope.readable.length}{" "}
          {scope.readable.length === 1 ? "namespace" : "namespaces"} this identity may read.
        </span>
      </section>
      <UnreadableNamespaces scopes={scope.scopes} />
      <TruncatedNotice namespaces={truncated} />
      <section aria-label="Filters" className="flex flex-col gap-2.5">
        <div className="flex flex-wrap items-center gap-2.5">
          <label className="flex h-10 min-w-[240px] flex-auto items-center gap-2 rounded-xl border border-line bg-card px-3 text-mute">
            <span aria-hidden="true">⌕</span>
            <input
              value={query.q}
              onChange={(event) => change({ q: event.target.value || undefined })}
              placeholder="Name, namespace, reason or artifact"
              aria-label="Search resources"
              className="min-w-0 flex-auto border-0 bg-transparent text-sm text-fg outline-0"
            />
          </label>
          <Segmented
            label="Kind"
            value={query.kind}
            onChange={(kind) => change({ kind: kind === "all" ? undefined : kind })}
            options={[
              { value: "all", label: "All", count: kindCounts.all },
              { value: "schema", label: "Schemas", count: kindCounts.schema },
              { value: "migration", label: "Migrations", count: kindCounts.migration },
            ]}
          />
        </div>
        <div className="flex flex-wrap items-center gap-2.5">
          <Segmented
            label="State"
            value={query.state}
            onChange={(state) => change({ state: state === "all" ? undefined : state })}
            options={[
              { value: "all", label: "Any state", count: stateCounts.all },
              ...resourceStates.map((s) => ({ ...s, count: stateCounts[s.value] })),
            ]}
          />
          <Segmented
            label="Sort"
            value={query.sort}
            onChange={(sort) => change({ sort: sort === "attention" ? undefined : sort })}
            options={sortOrders}
          />
        </div>
      </section>
      {loading ? (
        <Loading what="the resources" />
      ) : error ? (
        <ReadError error={error} what="the resources" />
      ) : resources.length === 0 ? (
        <EmptyState tone="off" title="No schema or migration here">
          Nothing in the namespaces this identity may read.
        </EmptyState>
      ) : rows.length === 0 ? (
        <EmptyState tone="off" title="Nothing matches">
          <span className="flex flex-col items-center gap-3">
            Try another state or clear the search.
            <Button onClick={() => onSearch({ sort: search.sort, size: search.size })}>
              Reset filters
            </Button>
          </span>
        </EmptyState>
      ) : (
        <section className="overflow-hidden rounded-[18px] border border-line bg-card shadow-card">
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-left text-sm">
              <caption className="sr-only">
                {filtered ? "Resources that match the filters" : "Resources"}
              </caption>
              <thead>
                <tr className="border-b border-line font-mono text-[10.5px] tracking-[0.14em] text-mute uppercase">
                  <th scope="col" className="px-4 py-2.5 font-medium">
                    Resource
                  </th>
                  <th scope="col" className="px-3 py-2.5 font-medium">
                    State
                  </th>
                  <th scope="col" className="hidden px-3 py-2.5 font-medium lg:table-cell">
                    Reason
                  </th>
                  <th scope="col" className="hidden px-3 py-2.5 font-medium xl:table-cell">
                    Lifecycle
                  </th>
                  <th scope="col" className="hidden px-3 py-2.5 font-medium xl:table-cell">
                    Artifact
                  </th>
                  <th scope="col" className="px-4 py-2.5 text-right font-medium">
                    Updated
                  </th>
                </tr>
              </thead>
              <tbody>
                {page.rows.map((resource) => (
                  <Row key={resource.ref.uid} resource={resource} now={now} />
                ))}
              </tbody>
            </table>
          </div>
          <Pager
            first={page.first}
            last={page.last}
            total={page.total}
            page={page.page}
            pages={page.pages}
            size={size}
            onPage={(next) => onSearch({ ...search, page: next > 1 ? next : undefined })}
            onSize={(next) =>
              onSearch({
                ...search,
                page: undefined,
                size: next === pageSizes[0] ? undefined : next,
              })
            }
          />
        </section>
      )}
    </>
  )
}

function Row({ resource, now }: { resource: ResourceView; now: Date }) {
  const state = stateLabel(resource)
  const updated = updatedAt(resource)
  return (
    <tr className="border-b border-line last:border-b-0 hover:bg-[color-mix(in_oklab,var(--pri)_5%,transparent)]">
      <td className="px-4 py-3">
        <Link {...resourceLink(resource)} className="flex flex-col gap-0.5 text-fg">
          <span className="font-mono font-semibold">{resource.name}</span>
          <span className="text-xs text-mute">
            {resource.namespace} · {resource.kind === "schema" ? "Schema" : "Migration"}
          </span>
        </Link>
      </td>
      <td className="px-3 py-3">
        <span className="inline-flex items-center gap-2 whitespace-nowrap">
          <StatusGlyph tone={state.tone} size={20} />
          {state.text}
        </span>
      </td>
      <td className="hidden max-w-[380px] px-3 py-3 text-fg2 lg:table-cell">
        {say(resource, now).text}
      </td>
      <td className="hidden px-3 py-3 xl:table-cell">
        <StepDots steps={steps(resource)} label={`Lifecycle of ${resource.name}`} />
      </td>
      <td
        className="hidden max-w-[220px] truncate px-3 py-3 font-mono text-xs text-fg2 xl:table-cell"
        title={resource.source || undefined}
      >
        {resource.sourceDigest ? shortDigest(resource.sourceDigest) : resource.source || "–"}
      </td>
      <td
        className="px-4 py-3 text-right font-mono text-xs whitespace-nowrap text-mute"
        title={updated?.toISOString()}
      >
        {updated ? ago(updated, now) : "–"}
      </td>
    </tr>
  )
}

function ago(at: Date, now: Date) {
  const minutes = Math.max(0, Math.round((now.getTime() - at.getTime()) / 60_000))
  if (minutes < 1) {
    return "just now"
  }
  if (minutes < 60) {
    return `${minutes} min ago`
  }
  const hours = Math.floor(minutes / 60)
  return hours < 48 ? `${hours} h ago` : `${Math.floor(hours / 24)} d ago`
}

interface PagerProps {
  first: number
  last: number
  total: number
  page: number
  pages: number
  size: number
  onPage: (page: number) => void
  onSize: (size: number) => void
}

function Pager({ first, last, total, page, pages, size, onPage, onSize }: PagerProps) {
  return (
    <nav
      aria-label="Pages"
      className="flex flex-wrap items-center gap-3 border-t border-line px-4 py-2.5 text-xs text-fg2"
    >
      <span role="status">
        {first}–{last} of {total}
      </span>
      <span className="flex-auto" />
      <span className="text-mute">Rows</span>
      <Segmented
        label="Rows per page"
        value={String(size)}
        onChange={(value) => onSize(Number(value))}
        options={pageSizes.map((n) => ({ value: String(n), label: String(n) }))}
      />
      <Button
        className="min-h-8! px-3! text-xs"
        disabled={page <= 1}
        onClick={() => onPage(page - 1)}
      >
        ← Previous
      </Button>
      <span>
        Page {page} of {pages}
      </span>
      <Button
        className="min-h-8! px-3! text-xs"
        disabled={page >= pages}
        onClick={() => onPage(page + 1)}
      >
        Next →
      </Button>
    </nav>
  )
}
