import { RouterProvider, type RouterHistory } from "@tanstack/react-router"
import { useState } from "react"

import { createAppRouter } from "./router"

export interface AppProps {
  /** Why the launch link could not sign this browser in, if it could not. */
  launchError?: string
  /** Tests pass a memory history; the app uses the browser's. */
  history?: RouterHistory
}

export function App({ launchError, history }: AppProps) {
  const [router] = useState(() => createAppRouter({ history, context: { launchError } }))
  return <RouterProvider router={router} />
}
