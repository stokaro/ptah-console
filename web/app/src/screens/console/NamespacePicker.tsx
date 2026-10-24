import { useId, useState } from "react"

import { Access, type NamespaceScope } from "../../gen/ptah/console/v1/resources_pb"
import { accessLabel, matching } from "../../model/scope"

export interface NamespacePickerProps {
  /** Every namespace the console offers, readable or not. */
  scopes: NamespaceScope[]
  selection: ReadonlySet<string>
  onChange: (selection: Set<string>) => void
  /** The identity may list more namespaces than the console offers. */
  truncated?: boolean
  /** The most namespaces the console offers. */
  max?: number
  /** Started with the namespaces to offer, so the cut is undone by naming them. */
  local?: boolean
}

/**
 * Chooses namespaces among the ones the console offers. A namespace the
 * identity may not read stays in the list, unselectable, with the reason, so
 * a short list is never mistaken for a quiet cluster.
 */
export function NamespacePicker({
  scopes,
  selection,
  onChange,
  truncated,
  max,
  local,
}: NamespacePickerProps) {
  const [query, setQuery] = useState("")
  const [selectedOnly, setSelectedOnly] = useState(false)
  const listId = useId()
  const readable = scopes.filter((scope) => scope.access === Access.OK).map((scope) => scope.name)
  const matched = new Set(
    matching(
      scopes.map((scope) => scope.name),
      query
    )
  )
  const matchingReadable = readable.filter((name) => matched.has(name))
  const shown = scopes.filter(
    (scope) => matched.has(scope.name) && (!selectedOnly || selection.has(scope.name))
  )
  const unselectedMatches = matchingReadable.filter((name) => !selection.has(name)).length
  const toggle = (name: string) => {
    const next = new Set(selection)
    if (next.has(name)) {
      next.delete(name)
    } else {
      next.add(name)
    }
    onChange(next)
  }
  const count = readable.filter((name) => selection.has(name)).length

  return (
    <div className="flex min-h-0 flex-col gap-2.5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-[13px] text-fg2" aria-live="polite">
          <b className="font-semibold text-fg">{count}</b> of {readable.length} readable selected
        </span>
        <button
          type="button"
          onClick={() => onChange(new Set(readable))}
          disabled={count === readable.length}
          className={smallButton}
        >
          Select all readable
        </button>
      </div>
      <label className="flex h-9 items-center gap-2 rounded-[10px] border border-line bg-bg px-2.5 text-mute">
        <span aria-hidden="true">⌕</span>
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={`Search ${scopes.length} ${scopes.length === 1 ? "namespace" : "namespaces"}…`}
          aria-label="Search namespaces"
          aria-controls={listId}
          className="min-w-0 flex-auto border-0 bg-transparent text-sm text-fg outline-0"
        />
      </label>
      <div className="flex flex-wrap gap-1.5">
        <button
          type="button"
          onClick={() => onChange(new Set([...selection, ...matchingReadable]))}
          disabled={unselectedMatches === 0}
          className={smallButton}
        >
          Select {matchingReadable.length} matching
        </button>
        <button
          type="button"
          onClick={() => onChange(new Set())}
          disabled={count === 0}
          className={smallButton}
        >
          Clear
        </button>
        <button
          type="button"
          aria-pressed={selectedOnly}
          onClick={() => setSelectedOnly(!selectedOnly)}
          className={`${smallButton} aria-pressed:border-pri aria-pressed:bg-pribg aria-pressed:text-pri`}
        >
          Selected only
        </button>
      </div>
      <ul
        id={listId}
        aria-label="Namespaces"
        className="m-0 flex min-h-0 list-none flex-col gap-0.5 overflow-y-auto p-0"
      >
        {shown.map((scope) => {
          const on = selection.has(scope.name) && scope.access === Access.OK
          const unreadable = scope.access !== Access.OK
          return (
            <li key={scope.name}>
              <button
                type="button"
                aria-pressed={on}
                disabled={unreadable}
                onClick={() => toggle(scope.name)}
                className="grid w-full cursor-pointer grid-cols-[18px_minmax(0,1fr)] items-center gap-2.5 rounded-[10px] border-0 bg-transparent px-2.5 py-2 text-left text-fg hover:bg-hover disabled:cursor-not-allowed disabled:opacity-70"
              >
                <span
                  aria-hidden="true"
                  className={`flex size-[18px] items-center justify-center rounded-[5px] border text-[11px] ${on ? "border-pri bg-pri text-onpri" : "border-line2 bg-card"}`}
                >
                  {on ? "✓" : ""}
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="truncate font-mono text-[13px] font-medium">{scope.name}</span>
                  <span className="text-xs text-mute">
                    {accessLabel(scope)}
                    {unreadable && scope.reason ? `: ${scope.reason}` : ""}
                  </span>
                </span>
              </button>
            </li>
          )
        })}
      </ul>
      {shown.length === 0 ? (
        <p className="m-0 px-1 text-sm text-fg2">
          {selectedOnly && query.trim() === ""
            ? "No namespace is selected."
            : `No namespace matches “${query.trim()}”.`}
        </p>
      ) : null}
      {truncated ? (
        <p className="m-0 rounded-[10px] bg-amberbg px-3 py-2 text-xs text-fg">
          This identity may list more namespaces than the console offers. It offers the first{" "}
          {max ?? "few"} by name
          {local ? "; start ptah-console local with --namespace for each one to work in." : "."}
        </p>
      ) : null}
    </div>
  )
}

const smallButton =
  "inline-flex h-7 cursor-pointer items-center rounded-lg border border-line bg-card px-2.5 text-[12.5px] font-medium text-fg disabled:cursor-not-allowed disabled:opacity-50"
