import { useCallback, useMemo, useSyncExternalStore } from "react"

import {
  choiceKey,
  choiceOf,
  inScope,
  parseChoice,
  serializeChoice,
  type ScopeChoice,
} from "../model/scope"
import { useCluster } from "./cluster"
import { useScope, useSession } from "./resources"

/**
 * Choices written while this page is open. A browser that refuses storage
 * still keeps a choice until the page is closed, and the newest write wins
 * over what storage held when the page opened.
 */
const written = new Map<string, string | null>()
const listeners = new Set<() => void>()

function read(key: string): string | null {
  if (written.has(key)) {
    return written.get(key) ?? null
  }
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string | null) {
  written.set(key, value)
  try {
    if (value === null) {
      window.localStorage.removeItem(key)
    } else {
      window.localStorage.setItem(key, value)
    }
  } catch {
    // A blocked storage only means the choice lasts for this page.
  }
  for (const listener of listeners) {
    listener()
  }
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  // Another tab's choice for the same cluster and identity is this one's too.
  const fromAnotherTab = (event: StorageEvent) => {
    if (event.key !== null && event.key.startsWith("ptah-console-scope:")) {
      written.delete(event.key)
      listener()
    }
  }
  window.addEventListener("storage", fromAnotherTab)
  return () => {
    listeners.delete(listener)
    window.removeEventListener("storage", fromAnotherTab)
  }
}

/** Forgets what this page wrote; for tests, which share one module between renders. */
export function forgetScopeChoices() {
  written.clear()
}

/**
 * The namespaces the viewer chose to work in, for the cluster the page shows
 * and the identity the console acts as, and the means to change them.
 */
export function useNamespaceScope() {
  const scope = useScope()
  const session = useSession()
  const { uid } = useCluster()
  const identity = session.isSuccess ? session.data : undefined
  const key = identity ? choiceKey(uid || identity.context, identity.kubernetesUsername) : undefined
  const raw = useSyncExternalStore(subscribe, () => (key ? read(key) : null))
  const choice = useMemo(() => parseChoice(raw), [raw])
  const readable = scope.readable
  const selected = inScope(readable, choice)
  const choose = useCallback(
    (selection: Iterable<string>) => {
      if (!key) {
        return
      }
      const next = choiceOf(selection, readable)
      write(key, next ? serializeChoice(next) : null)
    },
    [key, readable]
  )
  return {
    scope,
    ...selected,
    choice: choice as ScopeChoice | undefined,
    /** The scope and the session are known, so a choice can be read and made. */
    ready: scope.isSuccess && key !== undefined,
    /** Where the choice is kept, once the session says whose it is. */
    key,
    /** Whose choice to read is not known yet: the session is still being read. */
    pending: session.isPending,
    /** A choice is kept for this identity; false when the session could not say who it is. */
    keyed: key !== undefined,
    choose,
  }
}
