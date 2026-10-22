import { lazy } from "react"
import { timestampDate } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import { connectionName, reachable, useCluster } from "../api/cluster"
import { ChannelState, type Connection } from "../gen/ptah/console/v1/connections_pb"
import { ago } from "../model/connection"
import { useNow } from "./common"

/** How a connection's channel reads at a glance. */
export function channelTone(connection: Connection): { tone: Tone; label: string } {
  if (!connection.enabled) {
    return { tone: "off", label: "disabled" }
  }
  switch (connection.channel) {
    case ChannelState.IN_CLUSTER:
      return { tone: "ok", label: "this cluster" }
    case ChannelState.CONNECTED:
      return connection.rotationDue
        ? { tone: "warn", label: "connected, credential expires soon" }
        : { tone: "ok", label: "connected" }
    case ChannelState.DISCONNECTED:
      return { tone: "bad", label: "disconnected" }
    case ChannelState.QUARANTINED:
      return { tone: "bad", label: "quarantined" }
    default:
      return { tone: "unk", label: "awaiting the agent" }
  }
}

/**
 * Says when the cluster a page shows is out of reach: disconnected is not
 * failed, and a page that reads nothing from it says why rather than look
 * empty.
 */
export function ClusterBanner() {
  const { connection, external } = useCluster()
  const now = useNow(30_000)
  if (!external || !connection || reachable(connection)) {
    return null
  }
  const name = connectionName(connection)
  const since = connection.lastObservedAt
    ? `The console last heard from it ${ago(timestampDate(connection.lastObservedAt), now)}.`
    : "The console has not heard from it yet."
  const text = !connection.enabled
    ? `${name} is disabled. Its configuration is kept; nothing is read from it or sent to it.`
    : connection.channel === ChannelState.QUARANTINED
      ? `${name} is quarantined. Nothing is read from it or sent to it.`
      : `${name} is not connected. ${since} Nothing here is current, and no decision is sent to it until its agent is back.`
  return (
    <p
      role="status"
      className="m-0 rounded-xl border border-line bg-violetbg px-4 py-3 text-sm text-fg"
    >
      {text}
    </p>
  )
}

/** The cluster switcher, loaded apart: its menu is not needed to show a page. */
export const ClusterSwitcher = lazy(() =>
  import("./ClusterSwitcher").then((module) => ({ default: module.ClusterSwitcher }))
)
