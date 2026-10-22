import { timestampDate } from "@bufbuild/protobuf/wkt"
import type { Tone } from "@ptah/console-ui"

import { ChannelState, type Connection } from "../gen/ptah/console/v1/connections_pb"
import { Access, Answer, type ListNamespacesResponse } from "../gen/ptah/console/v1/resources_pb"

/** One of the four things the console shows apart for a connection. */
export interface Dimension {
  name: "Channel" | "Authorization" | "Namespaces" | "Operator"
  tone: Tone
  text: string
}

/** A connection as an administrator reads it. */
export interface ConnectionSummary {
  label: string
  tone: Tone
  dimensions: Dimension[]
  /** What to do about it, if anything. */
  todo: string
  /** A command that helps, if one does. */
  fix: string
}

/** How long ago a moment was, in words. */
export function ago(at: Date, now: Date) {
  const minutes = Math.max(0, Math.round((now.getTime() - at.getTime()) / 60_000))
  if (minutes < 1) {
    return "just now"
  }
  if (minutes < 60) {
    return `${minutes} min ago`
  }
  const hours = Math.round(minutes / 60)
  return hours < 48 ? `${hours} h ago` : `${Math.round(hours / 24)} days ago`
}

function lastObserved(connection: Connection, now: Date) {
  return connection.lastObservedAt
    ? `Last observed ${ago(timestampDate(connection.lastObservedAt), now)}`
    : "Not observed"
}

/**
 * What the console can say of a connection: from the registry alone, and
 * from what the person's own identity found there, when the cluster answered.
 * A cluster that is not reached says so; it is never shown as one with
 * nothing in it.
 */
export function summarize(
  connection: Connection,
  scope: ListNamespacesResponse | undefined,
  scopeError: string,
  now: Date
): ConnectionSummary {
  const observed = lastObserved(connection, now)
  const unobserved = (name: Dimension["name"]): Dimension => ({ name, tone: "off", text: observed })
  if (!connection.enabled) {
    return {
      label: "Disabled",
      tone: "off",
      dimensions: [
        { name: "Channel", tone: "off", text: "Disabled" },
        unobserved("Authorization"),
        unobserved("Namespaces"),
        unobserved("Operator"),
      ],
      todo: "Its configuration is kept. Enable it again at any time.",
      fix: "",
    }
  }
  switch (connection.channel) {
    case ChannelState.QUARANTINED:
      return {
        label: "Quarantined",
        tone: "bad",
        dimensions: [
          { name: "Channel", tone: "off", text: "Channels closed, credentials refused" },
          { name: "Authorization", tone: "off", text: "Blocked" },
          unobserved("Namespaces"),
          unobserved("Operator"),
        ],
        todo: "Access through the console is revoked. A new credential lifts it once the target's administrator installs it.",
        fix: "",
      }
    case ChannelState.NEVER_CONNECTED:
      return {
        label: "Awaiting the agent",
        tone: "unk",
        dimensions: [
          { name: "Channel", tone: "unk", text: "Waiting for the agent" },
          { name: "Authorization", tone: "off", text: "Not checked" },
          { name: "Namespaces", tone: "off", text: "Not checked" },
          { name: "Operator", tone: "off", text: "Not observed" },
        ],
        todo: "The target's administrator still has to install the agent.",
        fix: "",
      }
    case ChannelState.DISCONNECTED:
      return {
        label: "Disconnected",
        tone: "bad",
        dimensions: [
          { name: "Channel", tone: "bad", text: `No channel: ${observed.toLowerCase()}` },
          unobserved("Authorization"),
          unobserved("Namespaces"),
          unobserved("Operator"),
        ],
        todo: "Check the agent's Pod on the target. If the cluster is gone, quarantine or remove the connection.",
        fix: `kubectl --namespace ptah-console-agent get pods   # on ${connection.name}`,
      }
  }
  const channel: Dimension =
    connection.channel === ChannelState.IN_CLUSTER
      ? { name: "Channel", tone: "ok", text: "The cluster the console runs in" }
      : connection.rotationDue && connection.credentialExpiresAt
        ? {
            name: "Channel",
            tone: "warn",
            text: `Credential expires ${timestampDate(connection.credentialExpiresAt).toLocaleDateString()}`,
          }
        : { name: "Channel", tone: "ok", text: "Connected" }
  const dimensions = [channel, ...reached(connection, scope, scopeError)]
  if (channel.tone === "warn") {
    return {
      label: "Credential expires soon",
      tone: "warn",
      dimensions,
      todo: "Issue a new credential; the target's administrator writes it into the agent's Secret. The old one is accepted until an hour after the new one connects.",
      fix: "",
    }
  }
  const worst = dimensions.find((dimension) => dimension.tone !== "ok")
  return {
    label: worst
      ? worst.name === "Authorization"
        ? "Connected, no access"
        : "Connected, look closer"
      : "Ready",
    tone: worst ? worst.tone : "ok",
    dimensions,
    todo:
      worst?.name === "Authorization"
        ? "Ask the namespace's owner to bind a Ptah role to your account there. cluster-admin is not needed."
        : "",
    fix: "",
  }
}

/** Authorization, namespaces and operator, as the person's identity found them. */
function reached(
  connection: Connection,
  scope: ListNamespacesResponse | undefined,
  scopeError: string
): Dimension[] {
  if (scopeError) {
    return [
      { name: "Authorization", tone: "unk", text: `Could not ask: ${scopeError}` },
      { name: "Namespaces", tone: "unk", text: "Not checked" },
      { name: "Operator", tone: "unk", text: "Not checked" },
    ]
  }
  if (!scope) {
    return [
      { name: "Authorization", tone: "run", text: "Asking" },
      { name: "Namespaces", tone: "run", text: "Asking" },
      { name: "Operator", tone: "run", text: "Asking" },
    ]
  }
  const total = scope.namespaces.length || connection.namespaces.length
  const readable = scope.namespaces.filter((namespace) => namespace.access === Access.OK)
  const hidden = scope.namespaces
    .filter((namespace) => namespace.access !== Access.OK)
    .map((namespace) => namespace.name)
  const acting = readable.filter((namespace) =>
    Object.values(namespace.capabilities ?? {}).some((answer) => answer === Answer.ALLOWED)
  )
  const authorization: Dimension =
    acting.length === 0
      ? { name: "Authorization", tone: "bad", text: "No Ptah permissions for you here" }
      : { name: "Authorization", tone: "ok", text: `You may act in ${acting.length} of ${total}` }
  const namespaces: Dimension =
    hidden.length === 0
      ? { name: "Namespaces", tone: "ok", text: `All ${total} in scope are readable` }
      : readable.length === 0
        ? { name: "Namespaces", tone: "bad", text: `None readable: ${hidden.join(", ")}` }
        : { name: "Namespaces", tone: "warn", text: `Not readable: ${hidden.join(", ")}` }
  const api = scope.operatorApi
  const operator: Dimension = !api?.served
    ? { name: "Operator", tone: "bad", text: api?.problem || "The operator's API is not served" }
    : api.missing.length > 0
      ? { name: "Operator", tone: "warn", text: `Not served: ${api.missing.join(", ")}` }
      : { name: "Operator", tone: "ok", text: "The operator's API is served" }
  return [authorization, namespaces, operator]
}
