import type { Tone } from "@ptah/console-ui"

import type { LiveState } from "../api/live"
import { Mode } from "../gen/ptah/console/v1/info_pb"
import { Access, type NamespaceScope, type OperatorApi } from "../gen/ptah/console/v1/resources_pb"
import type { Punctuality } from "./operator"

/** One pre-flight check: a verdict, what it is about, and what the console saw. */
export interface Check {
  tone: Tone
  title: string
  detail: string
}

/** Whether the cluster serves the operator's API with every resource the console uses. */
export function apiCheck(api: OperatorApi | undefined): Check {
  const title = "Operator API"
  if (!api) {
    return { tone: "unk", title, detail: "Not asked yet." }
  }
  if (!api.served) {
    return { tone: "bad", title, detail: api.problem || "The cluster does not serve it." }
  }
  if (api.missing.length > 0) {
    return { tone: "warn", title, detail: `Served without ${api.missing.join(", ")}.` }
  }
  return {
    tone: "ok",
    title,
    detail: "operator.ptah.run/v1alpha1 is served, with every resource the console uses.",
  }
}

/** The minor of a Kubernetes version as discovery spells it, such as 1.36 for v1.36.4+k3s1. */
export function minorOf(version: string) {
  const match = /^v?(\d+)\.(\d+)/.exec(version)
  return match ? `${match[1]}.${match[2]}` : undefined
}

/** Whether the cluster runs a Kubernetes minor this console release was tested on. */
export function kubernetesCheck(version: string, tested: string[]): Check {
  const title = "Kubernetes"
  const minor = minorOf(version)
  if (!minor) {
    return { tone: "unk", title, detail: "The API server did not say which version it runs." }
  }
  if (tested.length === 0) {
    return {
      tone: "unk",
      title,
      detail: `${version}. This build records no Kubernetes minors it was tested on.`,
    }
  }
  return tested.includes(minor)
    ? { tone: "ok", title, detail: `${version}, a minor this console was tested on.` }
    : {
        tone: "warn",
        title,
        detail: `${version} is not among the minors this console was tested on: ${tested.join(", ")}.`,
      }
}

/** What the session acts as, and whether it sends decisions at all. */
export function sessionCheck(
  session:
    { mode: Mode; context: string; kubernetesUsername: string; readOnly: boolean } | undefined
): Check {
  const title = "Session"
  if (!session) {
    return { tone: "unk", title, detail: "Not read yet." }
  }
  const through =
    session.mode === Mode.LOCAL
      ? `through the kubeconfig, context ${session.context}`
      : "through impersonation by the hosted console"
  return session.readOnly
    ? {
        tone: "warn",
        title,
        detail: `Acts as ${session.kubernetesUsername} ${through}, read-only: it sends no decision.`,
      }
    : { tone: "ok", title, detail: `Acts as ${session.kubernetesUsername} ${through}.` }
}

/** How many of the offered namespaces the identity may read, and whether the list was cut. */
export function namespacesCheck(scopes: NamespaceScope[], truncated: boolean, max: number): Check {
  const title = "Namespaces"
  const readable = scopes.filter((scope) => scope.access === Access.OK).length
  const refused = scopes.length - readable
  if (scopes.length === 0) {
    return { tone: "bad", title, detail: "The console offers no namespace." }
  }
  if (readable === 0) {
    return { tone: "bad", title, detail: "This identity may read none of the namespaces offered." }
  }
  const parts = [`May read ${readable} of the ${scopes.length} offered.`]
  if (truncated) {
    parts.push(`The identity may list more; the console offers the first ${max}.`)
  }
  return { tone: refused > 0 || truncated ? "warn" : "ok", title, detail: parts.join(" ") }
}

/** Whether the page follows the cluster live. */
export function liveCheck(live: LiveState, watching: boolean): Check {
  const title = "Live updates"
  if (!watching) {
    return { tone: "off", title, detail: "Nothing to follow: no namespace is readable." }
  }
  switch (live.status) {
    case "live":
      return { tone: "ok", title, detail: "The page follows the cluster as it changes." }
    case "connecting":
      return { tone: "run", title, detail: "Connecting." }
    case "lost":
      return { tone: "warn", title, detail: "The watch was lost; the console is reconnecting." }
    case "denied":
      return {
        tone: "warn",
        title,
        detail: `This identity may not watch: ${live.reason ?? "refused"}. Pages read again when you load them.`,
      }
    default:
      return { tone: "bad", title, detail: "The session ended." }
  }
}

/** Whether the operator keeps the schedule its readable resources promise. */
export function scheduleCheck(punctual: Punctuality): Check {
  const title = "Operator schedule"
  if (punctual.judged === 0) {
    return { tone: "off", title, detail: "No readable resource promises a read to judge by." }
  }
  if (punctual.late.length > 0) {
    return {
      tone: "warn",
      title,
      detail: `${punctual.late.length} of ${punctual.judged} past their next read by more than five minutes.`,
    }
  }
  return {
    tone: "ok",
    title,
    detail: `${punctual.judged} ${punctual.judged === 1 ? "resource was" : "resources were"} read when promised.`,
  }
}

/**
 * The kubectl command that asks the API server what this page asks, for the
 * namespaces shown. A hosted console's identity is an impersonated account,
 * which kubectl reaches only with --as and the right to impersonate it.
 */
export function kubectlCommand(
  session: { mode: Mode; context: string; kubernetesUsername: string } | undefined,
  namespaces: string[]
) {
  const who =
    session === undefined
      ? ""
      : session.mode === Mode.LOCAL
        ? ` --context ${quote(session.context)}`
        : ` --as ${quote(session.kubernetesUsername)}`
  const list = namespaces.length > 0 ? namespaces.join(" ") : "<namespace>"
  return `for ns in ${list}; do echo "== $ns"; kubectl${who} auth can-i --list --namespace "$ns"; done`
}

/** Quotes a word for a POSIX shell when it holds anything but safe characters. */
function quote(word: string) {
  return /^[A-Za-z0-9_./:@-]+$/.test(word) ? word : `'${word.replaceAll("'", `'"'"'`)}'`
}
