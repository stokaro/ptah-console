import { RouterProvider } from "@tanstack/react-router"
import { useState } from "react"

import { createAppRouter, type AppRouter } from "./router"

export interface AppProps {
  /** Why the launch link could not sign this browser in, if it could not. */
  launchError?: string
  /**
   * Tests pass a router on a memory history, with its own context, so they
   * can wait for it to finish loading. The app makes one on the browser's.
   */
  router?: AppRouter
}

export function App({ launchError, router }: AppProps) {
  const [current] = useState(() => router ?? createAppRouter({ context: { launchError } }))
  return <RouterProvider router={current} />
}
