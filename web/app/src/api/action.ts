import type { Interceptor } from "@connectrpc/connect"

/** The header that marks a request a person made, which keeps the session alive. */
export const actionHeader = "Ptah-Action"

/** How long after a click or a key press a request still counts as the person's. */
const actionWindowMs = 3000

let lastInteraction = Number.NEGATIVE_INFINITY

/**
 * Notes when the person last clicked or pressed a key. Requests made shortly
 * after count as theirs; background refreshes and the live stream do not, so
 * they never keep an idle session alive.
 */
export function watchInteractions(target: Window = window) {
  const note = () => {
    lastInteraction = performance.now()
  }
  target.addEventListener("pointerdown", note, { capture: true })
  target.addEventListener("keydown", note, { capture: true })
  return () => {
    target.removeEventListener("pointerdown", note, { capture: true })
    target.removeEventListener("keydown", note, { capture: true })
  }
}

export const actionInterceptor: Interceptor = (next) => (request) => {
  if (performance.now() - lastInteraction < actionWindowMs) {
    request.header.set(actionHeader, "1")
  }
  return next(request)
}
