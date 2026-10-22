import { Code, ConnectError, type Transport } from "@connectrpc/connect"
import {
  addStaticKeyToTransport,
  skipToken,
  TransportProvider,
  useQuery,
  useTransport,
} from "@connectrpc/connect-query"
import { useSearch } from "@tanstack/react-router"
import { createContext, useContext, type ReactNode } from "react"

import {
  ChannelState,
  ConnectionService,
  type Connection,
} from "../gen/ptah/console/v1/connections_pb"
import { useHosted } from "./resources"

/** The header that names the cluster a request is for, by connection UID. */
export const connectionHeader = "Ptah-Connection"

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

/**
 * The cluster a page shows: a connection UID in the URL, so a link carries
 * the cluster it was copied from. No parameter is the cluster the console
 * runs in.
 */
export interface ClusterSearch {
  cluster?: string
}

export function readClusterSearch(search: Record<string, unknown>): ClusterSearch {
  const cluster = search.cluster
  return typeof cluster === "string" && uuid.test(cluster) ? { cluster } : {}
}

/** The transports of each connection, per console transport. */
const transports = new WeakMap<Transport, Map<string, Transport>>()

/**
 * A transport that names the connection on every request. Its key in the
 * query cache is the connection, so two clusters never share a cached
 * answer, whatever the request.
 */
export function connectionTransport(base: Transport, connection: string): Transport {
  let known = transports.get(base)
  if (!known) {
    known = new Map()
    transports.set(base, known)
  }
  const existing = known.get(connection)
  if (existing) {
    return existing
  }
  const named = (header: HeadersInit | undefined) => {
    const headers = new Headers(header)
    headers.set(connectionHeader, connection)
    return headers
  }
  const transport = addStaticKeyToTransport(
    {
      unary: (method, signal, timeoutMs, header, input, contextValues) =>
        base.unary(method, signal, timeoutMs, named(header), input, contextValues),
      stream: (method, signal, timeoutMs, header, input, contextValues) =>
        base.stream(method, signal, timeoutMs, named(header), input, contextValues),
    },
    `connection:${connection}`
  )
  known.set(connection, transport)
  return transport
}

/** The console's own transport, which names no cluster. */
const ConsoleTransport = createContext<Transport | undefined>(undefined)

/** The selected cluster's transport for everything below, and the console's own beside it. */
export function ClusterProvider({ children }: { children: ReactNode }) {
  const base = useTransport()
  const { cluster } = useSearch({ strict: false }) as ClusterSearch
  const transport = cluster ? connectionTransport(base, cluster) : base
  return (
    <ConsoleTransport.Provider value={base}>
      <TransportProvider transport={transport}>{children}</TransportProvider>
    </ConsoleTransport.Provider>
  )
}

/** The console's own transport: for its registry, never a cluster's. */
export function useConsoleTransport() {
  const own = useContext(ConsoleTransport)
  const current = useTransport()
  return own ?? current
}

/**
 * The connections the person may see. Only a hosted console with the
 * registry has them; elsewhere the list is empty and the console shows the
 * one cluster it reaches.
 */
export function useConnections() {
  const hosted = useHosted()
  const transport = useConsoleTransport()
  const query = useQuery(ConnectionService.method.listConnections, hosted ? {} : skipToken, {
    transport,
  })
  const absent =
    query.error !== null &&
    [Code.Unimplemented, Code.NotFound].includes(ConnectError.from(query.error).code)
  return {
    ...query,
    registry: query.isSuccess,
    absent,
    connections: query.data?.connections ?? [],
    manage: query.data?.manage ?? false,
    thisCluster: query.data?.thisCluster ?? "",
  }
}

/** The cluster the page shows, and its connection when the registry has one. */
export function useCluster() {
  const { cluster } = useSearch({ strict: false }) as ClusterSearch
  const { connections, thisCluster } = useConnections()
  const uid = cluster ?? thisCluster
  const connection: Connection | undefined = connections.find(
    (candidate) => candidate.connectionUid === uid
  )
  return { uid, external: cluster !== undefined && cluster !== thisCluster, connection }
}

/** A connection's name as a person reads it. */
export function connectionName(connection: Connection) {
  return connection.displayName || connection.name
}

/** Whether a connection serves requests now, as the console last saw. */
export function reachable(connection: Connection) {
  return (
    connection.enabled &&
    (connection.channel === ChannelState.IN_CLUSTER ||
      connection.channel === ChannelState.CONNECTED)
  )
}
