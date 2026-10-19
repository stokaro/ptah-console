import type { Transport } from "@connectrpc/connect"
import { TransportProvider } from "@connectrpc/connect-query"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { useState, type ReactNode } from "react"

import { createApiTransport } from "./api/transport"

export interface AppProvidersProps {
  children: ReactNode
  /** Tests pass an in-memory transport; the app uses the same-origin API. */
  transport?: Transport
  queryClient?: QueryClient
}

export function AppProviders({ children, transport, queryClient }: AppProvidersProps) {
  const [defaultTransport] = useState(createApiTransport)
  const [defaultQueryClient] = useState(() => new QueryClient())
  return (
    <TransportProvider transport={transport ?? defaultTransport}>
      <QueryClientProvider client={queryClient ?? defaultQueryClient}>
        {children}
      </QueryClientProvider>
    </TransportProvider>
  )
}
