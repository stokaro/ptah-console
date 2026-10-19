import { Code, ConnectError, createClient } from "@connectrpc/connect"
import { createConnectQueryKey, useTransport } from "@connectrpc/connect-query"
import { useQueryClient, type QueryClient } from "@tanstack/react-query"
import { createContext, use, useEffect, useState, type ReactNode } from "react"

import { DecisionService } from "../gen/ptah/console/v1/decisions_pb"
import { DiagnosticsService } from "../gen/ptah/console/v1/diagnostics_pb"
import { MigrationService, SchemaService } from "../gen/ptah/console/v1/resources_pb"
import { WatchService, WatchedKind } from "../gen/ptah/console/v1/watch_pb"

export type LiveStatus = "connecting" | "live" | "lost" | "ended"

export interface LiveState {
  status: LiveStatus
  /** When the server last said every watch was current. */
  lastHeartbeat?: Date
  /** When updates stopped, while they are stopped. */
  lostSince?: Date
  reason?: string
}

const LiveContext = createContext<LiveState>({ status: "connecting" })

/** Where live updates stand, for the banner and the freshness line. */
export function useLive() {
  return use(LiveContext)
}

const servicesFor: Record<number, Parameters<typeof createConnectQueryKey>[0]["schema"][]> = {
  [WatchedKind.SCHEMA]: [SchemaService, DiagnosticsService],
  [WatchedKind.MIGRATION]: [MigrationService, DiagnosticsService],
  [WatchedKind.SCHEMA_APPROVAL]: [DecisionService],
  [WatchedKind.MIGRATION_APPROVAL]: [DecisionService],
  [WatchedKind.RUN_ACKNOWLEDGMENT]: [DecisionService],
}

function invalidate(queryClient: QueryClient, kind: WatchedKind) {
  for (const schema of servicesFor[kind] ?? []) {
    void queryClient.invalidateQueries({
      queryKey: createConnectQueryKey({ schema, cardinality: undefined } as Parameters<
        typeof createConnectQueryKey
      >[0]),
    })
  }
}

/** Waits between attempts: 1, 2, 4, 8, 16, then 30 seconds. */
export function backoff(attempt: number) {
  return Math.min(30_000, 1000 * 2 ** Math.max(0, attempt - 1))
}

function sleep(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve) => {
    const timer = setTimeout(resolve, ms)
    signal.addEventListener("abort", () => {
      clearTimeout(timer)
      resolve()
    })
  })
}

/**
 * Follows the namespaces through WatchService and refetches what changed. The
 * stream carries no objects; every page reads them again through the API.
 */
export function LiveUpdates({
  namespaces,
  children,
}: {
  namespaces: string[]
  children: ReactNode
}) {
  const transport = useTransport()
  const queryClient = useQueryClient()
  const [state, setState] = useState<LiveState>({ status: "connecting" })
  const key = namespaces.join("\n")

  useEffect(() => {
    if (key === "") {
      return
    }
    const abort = new AbortController()
    const client = createClient(WatchService, transport)
    const watched = key.split("\n")
    let attempt = 0
    let behind = false

    async function follow() {
      while (!abort.signal.aborted) {
        try {
          for await (const response of client.subscribe(
            { namespaces: watched },
            { signal: abort.signal }
          )) {
            const event = response.event
            if (event.case === "heartbeat") {
              attempt = 0
              if (behind) {
                // Whatever changed while updates were stopped, read again.
                behind = false
                void queryClient.invalidateQueries()
              }
              setState({ status: "live", lastHeartbeat: new Date() })
            } else if (event.case === "invalidate") {
              invalidate(queryClient, event.value.kind)
            } else if (event.case === "lost") {
              behind = true
              const reason = event.value.reason
              setState((previous) => ({
                ...previous,
                status: "lost",
                lostSince: previous.lostSince ?? new Date(),
                reason,
              }))
            }
          }
        } catch (error) {
          if (abort.signal.aborted) {
            return
          }
          const failure = ConnectError.from(error)
          if (failure.code === Code.Unauthenticated) {
            setState({ status: "ended", reason: failure.rawMessage })
            void queryClient.invalidateQueries()
            return
          }
          behind = true
          setState((previous) => ({
            ...previous,
            status: "lost",
            lostSince: previous.lostSince ?? new Date(),
            reason: failure.rawMessage,
          }))
        }
        attempt += 1
        await sleep(backoff(attempt), abort.signal)
      }
    }
    void follow()
    return () => abort.abort()
  }, [key, transport, queryClient])

  return <LiveContext value={state}>{children}</LiveContext>
}
