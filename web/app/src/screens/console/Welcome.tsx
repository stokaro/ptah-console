import { Dialog } from "@base-ui/react/dialog"
import { StatusGlyph, Wordmark } from "@ptah/console-ui"
import { useNavigate } from "@tanstack/react-router"
import { useState } from "react"

import { accents } from "../../accent"
import { useAccent, useTheme } from "../../appearance"
import { connectionName, reachable, useCluster, useConnections } from "../../api/cluster"
import { useSession } from "../../api/resources"
import { useNamespaceScope } from "../../api/scope"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import { sessionSource, sourceName } from "../../model/session"
import { themes } from "../../theme"
import { channelTone } from "../clusters"
import { NamespacePicker } from "./NamespacePicker"

type Step = "cluster" | "namespaces" | "appearance"

const titles: Record<Step, { title: string; sub: string }> = {
  cluster: {
    title: "Pick a cluster",
    sub: "Each cluster has its own namespaces and its own choice of them.",
  },
  namespaces: {
    title: "Choose the namespaces to watch",
    sub: "The pages read only these. A namespace this identity may not read stays listed, with the reason.",
  },
  appearance: {
    title: "Set how it looks",
    sub: "Kept in this browser. The states keep their colors whatever the accent is.",
  },
}

export interface WelcomeProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Where the viewer wants to work, asked once per cluster and identity: the
 * cluster when there is more than one, the namespaces, and the appearance.
 * Closing it without finishing keeps every readable namespace for this
 * visit and asks again on the next one.
 */
export function Welcome({ open, onOpenChange }: WelcomeProps) {
  const scope = useNamespaceScope()
  const session = useSession()
  const { connections } = useConnections()
  const { uid } = useCluster()
  const steps: Step[] = [
    ...(connections.length > 1 ? ["cluster" as const] : []),
    "namespaces",
    "appearance",
  ]
  const [index, setIndex] = useState(0)
  const [draft, setDraft] = useState<{ cluster: string; selection: Set<string> }>()
  const step = steps[Math.min(index, steps.length - 1)]
  // Until the viewer changes it, the selection is what the pages read now.
  const selection = draft?.cluster === uid ? draft.selection : new Set(scope.namespaces)
  const chosen = scope.scope.readable.filter((name) => selection.has(name)).length
  const current = connections.find((connection) => connection.connectionUid === uid)
  const last = index >= steps.length - 1
  const blocked =
    (step === "cluster" && current !== undefined && !reachable(current)) ||
    (step === "namespaces" && (!scope.ready || chosen === 0))
  const hint =
    step === "namespaces"
      ? scope.ready
        ? `${chosen} of ${scope.scope.readable.length} readable selected`
        : "Reading the namespaces…"
      : step === "cluster" && current && !reachable(current)
        ? "This cluster is out of reach now"
        : ""
  const next = () => {
    if (!last) {
      setIndex(index + 1)
      return
    }
    scope.choose(selection)
    onOpenChange(false)
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-[color-mix(in_oklab,var(--bg)_90%,transparent)] backdrop-blur-[18px]" />
        <Dialog.Popup className="fixed inset-0 z-50 m-auto box-border flex h-fit max-h-[calc(100vh-32px)] w-[min(980px,calc(100vw-32px))] flex-col overflow-hidden rounded-[22px] border border-line bg-card shadow-card md:flex-row">
          <div className="flex flex-col gap-4 border-b border-line bg-[color-mix(in_oklab,var(--card)_55%,var(--bg))] p-6 md:w-[320px] md:flex-none md:border-r md:border-b-0">
            <Wordmark caption="operator console" />
            <span className="flex flex-col gap-2">
              <Dialog.Title className="m-0 font-display text-[24px] leading-[1.15] font-semibold tracking-[-0.03em]">
                Choose where you want to work
              </Dialog.Title>
              <Dialog.Description className="m-0 text-[13.5px] text-fg2">
                The console shows only what your Kubernetes identity can read. Pick the namespaces
                to watch; you can change them from the top bar at any time.
              </Dialog.Description>
            </span>
            <ol aria-label="Steps" className="m-0 flex list-none flex-col gap-2 p-0 text-[13px]">
              {steps.map((name, position) => (
                <li
                  key={name}
                  aria-current={position === index ? "step" : undefined}
                  className="flex items-center gap-2.5"
                >
                  <StatusGlyph
                    tone={position < index ? "ok" : position === index ? "run" : "off"}
                    size={20}
                    label={position < index ? "done" : position === index ? "current" : "to do"}
                  />
                  <span className={position === index ? "font-semibold text-fg" : "text-fg2"}>
                    {titles[name].title}
                  </span>
                </li>
              ))}
            </ol>
            {session.data ? (
              <span className="mt-auto truncate font-mono text-xs text-mute">
                {session.data.kubernetesUsername} · {sourceName[sessionSource(session.data)]}
              </span>
            ) : null}
          </div>
          <div className="flex min-h-0 min-w-0 flex-auto flex-col gap-4 p-6">
            <div className="flex flex-col gap-1">
              <span className="font-mono text-[11px] tracking-[0.14em] text-mute uppercase">
                Step {index + 1} of {steps.length}
              </span>
              <h3 className="m-0 text-[17px] font-semibold">{titles[step].title}</h3>
              <span className="text-[13px] text-fg2">{titles[step].sub}</span>
            </div>
            <div className="flex min-h-[280px] flex-col overflow-hidden">
              {/* A cluster picked here keeps the welcome open, whatever that cluster's choice is. */}
              {step === "cluster" ? <ClusterStep onPick={() => onOpenChange(true)} /> : null}
              {step === "namespaces" ? (
                scope.ready ? (
                  <NamespacePicker
                    scopes={scope.scope.scopes}
                    selection={selection}
                    onChange={(next) => setDraft({ cluster: uid, selection: next })}
                    truncated={scope.scope.data?.truncated}
                    max={scope.scope.data?.maxNamespaces}
                    local={session.data?.mode === Mode.LOCAL}
                  />
                ) : (
                  <p className="m-0 text-sm text-fg2">
                    Reading the namespaces this identity may see…
                  </p>
                )
              ) : null}
              {step === "appearance" ? <AppearanceStep /> : null}
            </div>
            <div className="mt-auto flex flex-wrap items-center gap-3 border-t border-line pt-4">
              {index > 0 ? (
                <button type="button" onClick={() => setIndex(index - 1)} className={secondary}>
                  Back
                </button>
              ) : null}
              <span className="flex-auto text-xs text-mute" aria-live="polite">
                {hint}
              </span>
              <button type="button" onClick={next} disabled={blocked} className={primary}>
                {last ? "Open the console" : "Continue"}
              </button>
            </div>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

/** The clusters the person may see; picking one shows its namespaces next. */
function ClusterStep({ onPick }: { onPick: () => void }) {
  const { connections, thisCluster } = useConnections()
  const { uid } = useCluster()
  const navigate = useNavigate()
  return (
    <ul aria-label="Clusters" className="m-0 flex list-none flex-col gap-1.5 overflow-y-auto p-0">
      {connections.map((connection) => {
        const state = channelTone(connection)
        const on = connection.connectionUid === uid
        return (
          <li key={connection.connectionUid}>
            <button
              type="button"
              aria-pressed={on}
              onClick={() => {
                onPick()
                void navigate({
                  to: "/console",
                  search: (previous) => ({
                    ...previous,
                    cluster:
                      connection.connectionUid === thisCluster
                        ? undefined
                        : connection.connectionUid,
                  }),
                })
              }}
              className="grid w-full cursor-pointer grid-cols-[24px_minmax(0,1fr)] items-center gap-3 rounded-xl border border-line bg-bg px-3 py-2.5 text-left text-fg aria-pressed:border-pri aria-pressed:bg-pribg"
            >
              <StatusGlyph tone={state.tone} size={20} label={state.label} />
              <span className="flex min-w-0 flex-col">
                <span className="font-semibold break-words">{connectionName(connection)}</span>
                <span className="text-xs text-mute">
                  {state.label} · {connection.namespaces.length}{" "}
                  {connection.namespaces.length === 1 ? "namespace" : "namespaces"}
                </span>
              </span>
            </button>
          </li>
        )
      })}
    </ul>
  )
}

function AppearanceStep() {
  const [theme, setTheme] = useTheme()
  const [accent, setAccent] = useAccent()
  return (
    <div className="flex flex-col gap-5">
      <Choices label="Theme" options={themes} value={theme} onChange={(value) => setTheme(value)} />
      <Choices
        label="Accent"
        options={accents}
        value={accent}
        onChange={(value) => setAccent(value)}
      />
    </div>
  )
}

function Choices<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: readonly { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
}) {
  return (
    <div className="flex flex-col gap-2">
      <span id={`welcome-${label}`} className="text-[13px] font-semibold">
        {label}
      </span>
      <div role="radiogroup" aria-labelledby={`welcome-${label}`} className="flex flex-wrap gap-2">
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={option.value === value}
            onClick={() => onChange(option.value)}
            className="h-9 cursor-pointer rounded-[10px] border border-line bg-bg px-3.5 text-[13px] font-medium text-fg aria-checked:border-pri aria-checked:bg-pribg aria-checked:text-pri"
          >
            {option.label}
          </button>
        ))}
      </div>
    </div>
  )
}

const primary =
  "inline-flex h-10 cursor-pointer items-center rounded-xl border-0 bg-pri px-4 text-[13.5px] font-semibold text-onpri disabled:cursor-not-allowed disabled:opacity-50"
const secondary =
  "inline-flex h-10 cursor-pointer items-center rounded-xl border border-line bg-card px-4 text-[13.5px] font-medium text-fg"
