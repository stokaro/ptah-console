import { ConnectError } from "@connectrpc/connect"
import { Card, Eyebrow } from "@ptah/console-ui"
import { useRouter } from "@tanstack/react-router"
import { useEffect, useState, type ReactNode } from "react"

import { Access, type NamespaceScope } from "../gen/ptah/console/v1/resources_pb"

/** The current time, moving every thirty seconds, for "waiting 12 min". */
export function useNow(intervalMs = 30_000) {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), intervalMs)
    return () => clearInterval(timer)
  }, [intervalMs])
  return now
}

/** Where a resource lives, as the person knows it: the context and the namespace. */
export function where(context: string | undefined, namespace: string) {
  return context ? `${context} / ${namespace}` : namespace
}

export interface HeroProps {
  eyebrow: string
  count: number
  label: string
  sub: string
  /** What RBAC lets the session do here, in Kubernetes' words. */
  capability: string
}

/** The top of a role's list: how many things, and on what grounds. */
export function Hero({ eyebrow, count, label, sub, capability }: HeroProps) {
  return (
    <section className="flex animate-[pt-rise_.4s_both] flex-col gap-1.5">
      <Eyebrow>{eyebrow}</Eyebrow>
      <h1 className="m-0 flex flex-wrap items-baseline gap-3 font-display text-[28px] leading-[1.1] font-semibold tracking-[-0.035em]">
        <span className="text-[44px] text-pri">{count}</span>
        {label}
      </h1>
      <span className="text-[13.5px] text-fg2">
        {sub} · based on what you can do: <code className="font-mono text-xs">{capability}</code>
      </span>
    </section>
  )
}

/** Namespaces the session may not read, said as such rather than shown as empty. */
export function UnreadableNamespaces({ scopes }: { scopes: NamespaceScope[] }) {
  const refused = scopes.filter((scope) => scope.access !== Access.OK)
  if (refused.length === 0) {
    return null
  }
  return (
    <p className="m-0 rounded-xl border border-line bg-card px-4 py-3 text-[13px] text-fg2">
      Not shown, because this identity may not read them:{" "}
      {refused.map((scope, index) => (
        <span key={scope.name}>
          {index > 0 ? ", " : ""}
          <code className="font-mono">{scope.name}</code>
        </span>
      ))}
      .
    </p>
  )
}

export function Loading({ what }: { what: string }) {
  return (
    <p role="status" className="m-0 flex items-center gap-3 p-4 text-sm text-fg2">
      <span className="box-border size-5 animate-[pt-spin_.8s_linear_infinite] rounded-full border-2 border-pri border-t-transparent" />
      Reading {what}…
    </p>
  )
}

/** A failed read, in the server's words. */
export function ReadError({ error, what }: { error: unknown; what: string }) {
  return (
    <Card title={`Could not read ${what}`}>
      <span role="alert" className="text-sm text-red">
        {ConnectError.from(error).rawMessage}
      </span>
    </Card>
  )
}

export function BackButton() {
  const router = useRouter()
  return (
    <button
      type="button"
      onClick={() => router.history.back()}
      className="inline-flex min-h-10 cursor-pointer items-center gap-1.5 self-start rounded-[11px] border border-line bg-card py-0 pr-3 pl-2 text-[13.5px] font-medium text-fg"
    >
      ‹ Back
    </button>
  )
}

export function DetailHeading({
  eyebrow,
  title,
  sub,
  mono = true,
}: {
  eyebrow: string
  title: string
  sub?: ReactNode
  mono?: boolean
}) {
  return (
    <div className="flex flex-col gap-1">
      <Eyebrow>{eyebrow}</Eyebrow>
      <h1
        className={`m-0 text-[26px] leading-[1.1] font-semibold tracking-[-0.02em] break-words ${mono ? "font-mono" : "font-display"}`}
      >
        {title}
      </h1>
      {sub ? <span className="text-sm text-fg2">{sub}</span> : null}
    </div>
  )
}

/** Copies text, and says whether it worked. */
export async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
