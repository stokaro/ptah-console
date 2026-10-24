import { Autocomplete } from "@base-ui/react/autocomplete"
import { Dialog } from "@base-ui/react/dialog"
import { StatusGlyph, type Tone } from "@ptah/console-ui"
import { useNavigate } from "@tanstack/react-router"
import { useEffect } from "react"

import { useResources } from "../../api/resources"
import { stateLabel } from "../../model/catalog"

/** One place the palette can take the person. */
interface Destination {
  value: string
  label: string
  /** Where it is or what it is, shown beside the label and searched with it. */
  hint: string
  tone?: Tone
  go: () => void
}

interface Group {
  value: string
  items: Destination[]
}

export interface PaletteProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Jump to a page or a resource by typing part of its name. The results are
 * the console's own pages and the resources the pages already read; the
 * palette asks the server for nothing.
 */
export function Palette({ open, onOpenChange }: PaletteProps) {
  const navigate = useNavigate()
  const { resources } = useResources()
  const close = (go: () => void) => () => {
    onOpenChange(false)
    go()
  }
  const pages: Destination[] = [
    {
      value: "page:overview",
      label: "Overview",
      hint: "full console",
      go: () => void navigate({ to: "/console" }),
    },
    {
      value: "page:resources",
      label: "Resources",
      hint: "full console",
      go: () => void navigate({ to: "/console/resources" }),
    },
    {
      value: "page:preflight",
      label: "Pre-flight",
      hint: "full console",
      go: () => void navigate({ to: "/console/preflight" }),
    },
    {
      value: "page:activity",
      label: "Activity",
      hint: "full console",
      go: () => void navigate({ to: "/console/activity" }),
    },
    {
      value: "page:new-schema",
      label: "New schema",
      hint: "full console",
      go: () => void navigate({ to: "/console/new", search: { kind: "schema", template: "oci" } }),
    },
    {
      value: "page:new-migration",
      label: "New migration",
      hint: "full console",
      go: () =>
        void navigate({ to: "/console/new", search: { kind: "migration", template: "oci" } }),
    },
    {
      value: "page:architecture",
      label: "Architecture & safety",
      hint: "full console",
      go: () => void navigate({ to: "/console/architecture" }),
    },
    {
      value: "page:approve",
      label: "Approver",
      hint: "focused view",
      go: () => void navigate({ to: "/approve" }),
    },
    {
      value: "page:releases",
      label: "Developer",
      hint: "focused view",
      go: () => void navigate({ to: "/releases" }),
    },
    {
      value: "page:incidents",
      label: "On call",
      hint: "focused view",
      go: () => void navigate({ to: "/incidents" }),
    },
    {
      value: "page:decisions",
      label: "Auditor",
      hint: "focused view",
      go: () => void navigate({ to: "/decisions" }),
    },
  ]
  const resourceItems = (kind: "schema" | "migration"): Destination[] =>
    resources
      .filter((resource) => resource.kind === kind)
      .sort((a, b) => a.name.localeCompare(b.name) || a.namespace.localeCompare(b.namespace))
      .map((resource) => {
        const state = stateLabel(resource)
        const params = { namespace: resource.namespace, name: resource.name }
        return {
          value: `${kind}:${resource.namespace}/${resource.name}`,
          label: resource.name,
          hint: `${resource.namespace} · ${state.text}`,
          tone: state.tone,
          go: () =>
            void navigate(
              kind === "schema"
                ? { to: "/console/schemas/$namespace/$name", params }
                : { to: "/console/migrations/$namespace/$name", params }
            ),
        }
      })
  const groups: Group[] = [
    { value: "Pages", items: pages },
    { value: "Schemas", items: resourceItems("schema") },
    { value: "Migrations", items: resourceItems("migration") },
  ].filter((group) => group.items.length > 0)

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-[color-mix(in_oklab,var(--bg)_55%,transparent)] backdrop-blur-[3px]" />
        <Dialog.Popup className="fixed top-[12vh] left-1/2 z-50 flex max-h-[70vh] w-[min(640px,calc(100vw-32px))] -translate-x-1/2 flex-col overflow-hidden rounded-[18px] border border-line bg-card shadow-card">
          <Dialog.Title className="sr-only">Jump to a page or a resource</Dialog.Title>
          <Autocomplete.Root
            open
            inline
            items={groups}
            autoHighlight="always"
            keepHighlight
            itemToStringValue={(item: Destination) => `${item.label} ${item.hint}`}
          >
            <div className="flex items-center gap-2 border-b border-line px-4">
              <span aria-hidden="true" className="text-mute">
                ⌕
              </span>
              <Autocomplete.Input
                aria-label="Search pages and resources"
                placeholder="Search pages and resources"
                className="h-12 min-w-0 flex-auto border-0 bg-transparent text-[15px] text-fg outline-0"
              />
              <kbd className="font-mono text-[11px] text-mute">Esc</kbd>
            </div>
            <div className="overflow-y-auto p-2">
              <Autocomplete.Empty>
                <p className="m-0 px-3 py-4 text-sm text-fg2">Nothing matches.</p>
              </Autocomplete.Empty>
              <Autocomplete.List>
                {(group: Group) => (
                  <Autocomplete.Group key={group.value} items={group.items} className="pb-1.5">
                    <Autocomplete.GroupLabel className="px-3 pt-2 pb-1 font-mono text-[10px] font-medium tracking-[0.16em] text-mute uppercase">
                      {group.value}
                    </Autocomplete.GroupLabel>
                    <Autocomplete.Collection>
                      {(item: Destination) => (
                        <Autocomplete.Item
                          key={item.value}
                          value={item}
                          onClick={close(item.go)}
                          className="flex cursor-pointer items-center gap-2.5 rounded-[10px] px-3 py-2 text-sm data-[highlighted]:bg-pribg"
                        >
                          {item.tone ? <StatusGlyph tone={item.tone} size={20} /> : null}
                          <span className="font-medium">{item.label}</span>
                          <span className="ml-auto truncate text-xs text-mute">{item.hint}</span>
                        </Autocomplete.Item>
                      )}
                    </Autocomplete.Collection>
                  </Autocomplete.Group>
                )}
              </Autocomplete.List>
            </div>
          </Autocomplete.Root>
          <p className="m-0 border-t border-line px-4 py-2 text-xs text-mute">
            Arrow keys move, Enter opens. Only pages and the resources already read are searched.
          </p>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

/**
 * Opens the palette on ⌘K or Ctrl+K anywhere, and on / outside a field, as a
 * reader of other consoles expects.
 */
export function usePaletteKeys(open: () => void) {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const typing = target?.closest("input, textarea, select, [contenteditable='true']")
      if (event.key.toLowerCase() === "k" && (event.metaKey || event.ctrlKey) && !event.altKey) {
        event.preventDefault()
        open()
        return
      }
      if (event.key === "/" && !typing && !event.metaKey && !event.ctrlKey && !event.altKey) {
        event.preventDefault()
        open()
      }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [open])
}
