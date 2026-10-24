import { Popover } from "@base-ui/react/popover"
import { useState } from "react"

import { useSession } from "../../api/resources"
import { useNamespaceScope } from "../../api/scope"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import { NamespacePicker } from "./NamespacePicker"

/**
 * The namespaces the pages read, in the top bar, so a short list is never
 * mistaken for a quiet cluster; it opens the picker to change them. A change
 * applies only when the viewer says so, and never to none.
 */
export function NamespaceScopeChip() {
  const scope = useNamespaceScope()
  const session = useSession()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<Set<string>>(new Set())
  const scopes = scope.scope.scopes
  // Until the session says whose choice it is, the chip would name a scope
  // nothing reads.
  if (scopes.length === 0 || scope.pending) {
    return null
  }
  const readable = scope.scope.readable
  const shown = scope.namespaces
  const label = shown.length === 1 ? shown[0] : `${shown.length} of ${readable.length}`
  const chosen = readable.filter((name) => draft.has(name)).length
  return (
    <Popover.Root
      open={open}
      onOpenChange={(next) => {
        if (next) {
          setDraft(new Set(shown))
        }
        setOpen(next)
      }}
    >
      <Popover.Trigger
        aria-label={`Namespaces: ${shown.length} of ${readable.length} readable shown. Choose`}
        className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-full border border-line bg-card px-2.5 text-xs text-fg"
      >
        <span className="text-mute">namespaces</span>
        <b className="font-mono font-semibold">{label}</b>
        <span aria-hidden="true" className="text-mute">
          ▾
        </span>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner sideOffset={6} align="start" className="z-40">
          <Popover.Popup className="flex max-h-[min(560px,calc(100vh-96px))] w-[min(380px,calc(100vw-32px))] flex-col gap-3 rounded-[14px] border border-line bg-card p-3 shadow-card">
            <Popover.Title className="m-0 text-sm font-semibold">Namespaces to show</Popover.Title>
            {scope.lapsed ? (
              <p className="m-0 rounded-[10px] bg-amberbg px-3 py-2 text-xs text-fg">
                None of the namespaces chosen before is readable now, so every readable one is
                shown.
              </p>
            ) : null}
            <NamespacePicker
              scopes={scopes}
              selection={draft}
              onChange={setDraft}
              truncated={scope.scope.data?.truncated}
              max={scope.scope.data?.maxNamespaces}
              local={session.data?.mode === Mode.LOCAL}
            />
            <div className="flex items-center justify-end gap-2 border-t border-line pt-3">
              <Popover.Close className="inline-flex h-8 cursor-pointer items-center rounded-lg border border-line bg-card px-3 text-[13px] font-medium text-fg">
                Cancel
              </Popover.Close>
              <button
                type="button"
                disabled={chosen === 0}
                onClick={() => {
                  scope.choose(draft)
                  setOpen(false)
                }}
                className="inline-flex h-8 cursor-pointer items-center rounded-lg border-0 bg-pri px-3 text-[13px] font-semibold text-onpri disabled:cursor-not-allowed disabled:opacity-50"
              >
                {chosen === 0
                  ? "Choose at least one"
                  : `Show ${chosen} ${chosen === 1 ? "namespace" : "namespaces"}`}
              </button>
            </div>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
