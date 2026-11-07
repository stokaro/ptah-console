import { waitFor } from "@testing-library/react"

/** What a settle reads of a router: whether it is loading, and what. */
export interface LoadingRouter {
  readonly state: { readonly status: string; readonly location: { readonly href: string } }
}

const rendered = new Set<LoadingRouter>()

/** Makes the cleanup after this test wait until the router has finished loading. */
export function settleAfterTest<R extends LoadingRouter>(router: R): R {
  rendered.add(router)
  return router
}

/**
 * Waits until the router is idle. It has to run before the router's tree
 * unmounts.
 *
 * A navigation can outlive the assertion that followed it: the page's lazy
 * chunk, and the minimum time a pending fallback stays up, resolve later. The
 * router then commits through React. Once the tree is gone that commit never
 * finishes, and when it runs after Vitest has torn down jsdom, React reads
 * `window` and the file fails with an unhandled rejection after every test in
 * it passed.
 */
export async function settle(router: LoadingRouter): Promise<void> {
  rendered.delete(router)
  await waitFor(() => {
    if (router.state.status !== "idle") {
      throw new Error(`The router is still loading ${router.state.location.href}`)
    }
  })
}

/** Settles every router the test rendered and has not settled yet. */
export async function settleRouters(): Promise<void> {
  for (const router of [...rendered]) {
    await settle(router)
  }
}
