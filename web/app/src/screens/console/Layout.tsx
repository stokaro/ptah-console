import { Menu } from "@base-ui/react/menu"
import { Icon, StatusGlyph, Wordmark, type IconName, type Tone } from "@ptah/console-ui"
import { Link, Outlet } from "@tanstack/react-router"
import { Suspense, useCallback, useState } from "react"

import { accents, type AccentChoice } from "../../accent"
import { useAccent } from "../../appearance"
import { useConnections } from "../../api/cluster"
import { useInfo, useResources, useScope, useSession } from "../../api/resources"
import { useNamespaceScope } from "../../api/scope"
import { NamespaceSelection } from "../../api/selection"
import { BuildFooter } from "../../BuildFooter"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import { punctuality } from "../../model/operator"
import { needsAttention } from "../../model/overview"
import { sessionSource, sourceName } from "../../model/session"
import { ClusterBanner, ClusterSwitcher } from "../clusters"
import { useNow } from "../common"
import { LiveBanner, OperatorApiBanner, SessionChips, SignOut, ThemeSwitch } from "../Shell"
import { resourceLink } from "./links"
import { NamespaceScopeChip } from "./NamespaceScope"
import { Palette, usePaletteKeys } from "./Palette"
import { Welcome } from "./Welcome"
import { WelcomeControl } from "./welcomeControl"

interface Page {
  to:
    | "/console"
    | "/console/resources"
    | "/console/activity"
    | "/console/architecture"
    | "/console/preflight"
    | "/connections"
  label: string
  icon: IconName
  /** What the number beside the label counts. */
  counts?: "attention" | "resources"
}

/** The full console's pages, by group, in the order the navigation lists them. */
const groups: { label: string; pages: Page[] }[] = [
  {
    label: "Workspace",
    pages: [
      { to: "/console", label: "Overview", icon: "overview", counts: "attention" },
      { to: "/console/resources", label: "Resources", icon: "resources", counts: "resources" },
    ],
  },
  {
    label: "Installation",
    pages: [
      { to: "/console/activity", label: "Activity", icon: "activity" },
      { to: "/console/architecture", label: "Architecture & safety", icon: "safety" },
      { to: "/console/preflight", label: "Pre-flight", icon: "preflight" },
    ],
  },
]

/** Connections are the registry's; they are listed to who may change it. */
const connectionsPage: Page = { to: "/connections", label: "Connections", icon: "connections" }

/**
 * The full console: every resource the session may read, with a sidebar to
 * move between its pages. The focused view stays one link away.
 */
export function ConsoleLayout() {
  const session = useSession()
  const hosted = session.isSuccess && session.data.mode === Mode.HOSTED
  const [palette, setPalette] = useState(false)
  usePaletteKeys(useCallback(() => setPalette(true), []))
  const scope = useNamespaceScope()
  const clusters = useConnections()
  // The welcome asks once per cluster and identity, when there is a choice to
  // make. Closed unfinished, it asks again on the next visit, not this one.
  const [closedFor, setClosedFor] = useState<string>()
  const [held, setHeld] = useState(false)
  // It waits for the clusters too, so its steps do not change under the viewer.
  const asks =
    scope.ready &&
    clusters.known &&
    scope.choice === undefined &&
    scope.scope.readable.length > 1 &&
    closedFor !== scope.key
  const welcome = held || asks
  const setWelcome = (open: boolean) => {
    setHeld(open)
    if (!open) {
      setClosedFor(scope.key)
    }
  }
  // Until the session says whose choice to read, nothing is read; a session
  // that could not say reads every readable namespace.
  const selection = scope.pending ? null : scope.keyed ? scope.namespaces : undefined
  return (
    <NamespaceSelection.Provider value={selection}>
      <WelcomeControl.Provider value={() => setWelcome(true)}>
        <Frame hosted={hosted} openPalette={() => setPalette(true)} />
        <Palette open={palette} onOpenChange={setPalette} />
        <Welcome open={welcome} onOpenChange={setWelcome} />
      </WelcomeControl.Provider>
    </NamespaceSelection.Provider>
  )
}

function Frame({ hosted, openPalette }: { hosted: boolean; openPalette: () => void }) {
  return (
    <div className="flex min-h-screen flex-col md:flex-row">
      {/* One navigation: a strip across the top on a phone, a sidebar on a wider screen. */}
      <aside className="flex flex-wrap items-center gap-3 border-b border-line bg-[color-mix(in_oklab,var(--card)_55%,var(--bg))] px-4 py-2.5 md:sticky md:top-0 md:h-screen md:w-[248px] md:flex-none md:flex-col md:flex-nowrap md:items-stretch md:gap-5 md:overflow-y-auto md:border-r md:border-b-0 md:px-3.5 md:py-4">
        <Link to="/console" aria-label="Ptah Console, full console" className="px-1.5 text-fg">
          <Wordmark caption="console" />
        </Link>
        <ConsoleNav />
        <span className="hidden flex-auto md:block" />
        <OperatorStatus />
        <Link
          to="/"
          className="flex items-center gap-2.5 rounded-xl border border-line bg-card px-3 py-2 text-[13px] font-semibold text-fg"
        >
          <Icon name="focus" className="text-mute" />
          <span className="flex flex-col gap-0.5">
            Focused view
            <span className="text-xs font-normal text-mute">by role</span>
          </span>
        </Link>
        <Identity />
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex flex-wrap items-center gap-3 border-b border-line bg-[color-mix(in_oklab,var(--bg)_85%,transparent)] px-4 py-3 backdrop-blur-[14px] md:px-6">
          <Suspense fallback={null}>
            <ClusterSwitcher />
          </Suspense>
          <SessionChips identityOnNarrowOnly />
          <NamespaceScopeChip />
          <span className="flex-auto" />
          <button
            type="button"
            onClick={openPalette}
            className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-full border border-line bg-card px-3 text-xs text-fg2"
          >
            <Icon name="search" size={14} />
            Search or jump to
            <kbd className="font-mono text-[11px] text-mute">⌘K</kbd>
          </button>
          {hosted ? (
            <span className="md:hidden">
              <SignOut />
            </span>
          ) : null}
          <AccentSwitch />
          <ThemeSwitch />
        </header>
        <main className="mx-auto box-border flex w-full max-w-[1400px] flex-1 flex-col gap-4 px-4 pt-[22px] pb-[72px] md:px-6">
          <ClusterBanner />
          <OperatorApiBanner />
          <LiveBanner />
          <Outlet />
        </main>
        <BuildFooter />
      </div>
    </div>
  )
}

function ConsoleNav() {
  const { resources, loading } = useResources()
  const { manage } = useConnections()
  const counts = {
    attention: needsAttention(resources).length,
    resources: resources.length,
  }
  const shown = manage
    ? groups.map((group) =>
        group.label === "Installation"
          ? { ...group, pages: [...group.pages, connectionsPage] }
          : group
      )
    : groups
  return (
    <nav
      aria-label="Console"
      className="flex flex-auto flex-wrap gap-x-4 gap-y-1 md:flex-none md:flex-col md:gap-4"
    >
      {shown.map((group) => (
        <div
          key={group.label}
          role="group"
          aria-labelledby={`console-nav-${group.label}`}
          className="flex flex-wrap gap-1 md:flex-col"
        >
          <span
            id={`console-nav-${group.label}`}
            className="hidden px-3 font-mono text-[10px] tracking-[0.14em] text-mute uppercase md:block"
          >
            {group.label}
          </span>
          {group.pages.map((page) => {
            const count = page.counts && !loading ? counts[page.counts] : undefined
            const counted =
              count === undefined ? undefined : `${page.label}, ${counting(page.counts, count)}`
            return (
              <Link
                key={page.to}
                to={page.to}
                aria-label={counted}
                activeOptions={{ exact: true, includeSearch: false }}
                className="flex min-h-10 items-center gap-2.5 rounded-[10px] border border-transparent px-3 text-[13.5px] font-medium text-fg2"
                activeProps={{
                  className: "border-line! bg-card text-fg! shadow-card",
                  "aria-current": "page",
                }}
              >
                <Icon name={page.icon} className="text-mute" />
                <span className="flex-auto">{page.label}</span>
                {count === undefined ? null : (
                  <span
                    className={`font-mono text-[11px] ${page.counts === "attention" && count > 0 ? "text-amber" : "text-mute"}`}
                  >
                    {count}
                  </span>
                )}
              </Link>
            )
          })}
        </div>
      ))}
    </nav>
  )
}

/** The number beside a page's label, said in words for its accessible name. */
function counting(counts: Page["counts"], count: number) {
  if (counts === "attention") {
    return `${count} ${count === 1 ? "needs" : "need"} attention`
  }
  return `${count} ${count === 1 ? "resource" : "resources"}`
}

/**
 * Whether the operator keeps the schedule the readable resources promise,
 * and the release this console was built against. The console does not ask
 * the cluster which operator runs: a schedule its resources stopped keeping
 * is the evidence it has.
 */
export function OperatorStatus() {
  const scope = useScope()
  const info = useInfo()
  const { resources, loading } = useResources()
  const now = useNow(30_000)
  const api = scope.data?.operatorApi
  const { judged, late } = punctuality(resources, now)
  let tone: Tone = "ok"
  let headline = "on schedule"
  let detail = `${judged} ${judged === 1 ? "resource was" : "resources were"} read when promised`
  if (api && !(api.served && api.missing.length === 0)) {
    tone = "bad"
    headline = "API not served"
    detail = api.problem
  } else if (loading) {
    tone = "off"
    headline = "reading"
    detail = "Reading the resources it reconciles"
  } else if (judged === 0) {
    tone = "off"
    headline = "nothing to judge by"
    detail = "No readable resource promises a read"
  } else if (late.length > 0) {
    tone = "warn"
    headline = `${late.length} past their next read`
    detail = `By more than five minutes, of ${judged}`
  }
  return (
    <section
      aria-label="Operator schedule"
      className="hidden flex-col gap-1.5 rounded-xl border border-line bg-card px-3 py-2.5 text-[13px] md:flex"
    >
      <span className="flex items-center gap-2">
        <StatusGlyph tone={tone} size={20} />
        <span className="flex min-w-0 flex-col">
          <b className="font-semibold">
            Operator <span className="font-normal text-fg2">· {headline}</span>
          </b>
          <span className="text-xs text-mute">{detail}</span>
        </span>
      </span>
      {late[0] ? (
        <Link {...resourceLink(late[0])} className="truncate text-xs font-medium text-pri">
          Latest: {late[0].namespace}/{late[0].name}
        </Link>
      ) : null}
      {info.data?.operatorVersion ? (
        <span className="font-mono text-[11px] text-mute">
          console built against {info.data.operatorVersion}
        </span>
      ) : null}
    </section>
  )
}

/** Who the console acts as, at the foot of the sidebar on a wide screen. */
function Identity() {
  const session = useSession()
  // A failed refresh keeps the last session's data; it names nobody now.
  const identity = session.isSuccess ? session.data : undefined
  if (!identity) {
    return null
  }
  return (
    <section aria-label="Acting as" className="hidden items-center gap-2.5 px-1 md:flex">
      <span
        aria-hidden="true"
        className="flex size-8 flex-none items-center justify-center rounded-full bg-pribg font-mono text-[11px] font-semibold text-pri"
      >
        {initials(identity.displayName || identity.kubernetesUsername)}
      </span>
      <span className="flex min-w-0 flex-auto flex-col">
        <b className="truncate font-mono text-xs font-semibold" title={identity.kubernetesUsername}>
          {identity.kubernetesUsername}
        </b>
        <span className="truncate text-[11px] text-mute">
          {sourceName[sessionSource(identity)]} · {identity.context}
        </span>
      </span>
      {identity.mode === Mode.HOSTED ? <SignOut /> : null}
    </section>
  )
}

/**
 * Up to two letters for a name: the first letters of its first two words, or
 * the first two letters of its only one. A Kubernetes user name's prefix, such
 * as system:serviceaccount:, names no person and is left out.
 */
export function initials(name: string) {
  const words = name
    .replace(/^.*:/, "")
    .split(/[\s._@-]+/)
    .filter(Boolean)
  const letters = words.length > 1 ? `${words[0][0]}${words[1][0]}` : (words[0] ?? "?").slice(0, 2)
  return letters.toUpperCase()
}

/** The accent a viewer chose, for actions, links and focus; the states keep their colors. */
function AccentSwitch() {
  const [accent, setAccent] = useAccent()
  const current = accents.find((option) => option.value === accent) ?? accents[0]
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label={`Accent color: ${current.label}. Choose another`}
        className="inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-full border border-line bg-card px-2.5 text-xs text-fg2"
      >
        <span aria-hidden="true" className="size-3 rounded-full bg-pri" />
        Accent
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={6} align="end" className="z-40">
          <Menu.Popup className="flex max-w-[280px] min-w-[220px] flex-col rounded-[14px] border border-line bg-card p-1 shadow-card">
            <Menu.RadioGroup
              value={accent}
              onValueChange={(value) => setAccent(value as AccentChoice)}
            >
              <Menu.GroupLabel className="px-3 pt-2 pb-1 font-mono text-[10px] tracking-[0.14em] text-mute uppercase">
                Accent color
              </Menu.GroupLabel>
              {accents.map((option) => (
                <Menu.RadioItem
                  closeOnClick
                  key={option.value}
                  value={option.value}
                  className="flex cursor-pointer items-center gap-2.5 rounded-[10px] px-3 py-2 text-sm data-[highlighted]:bg-hover"
                >
                  <span className="flex-auto">{option.label}</span>
                  <Menu.RadioItemIndicator className="text-pri">✓</Menu.RadioItemIndicator>
                </Menu.RadioItem>
              ))}
            </Menu.RadioGroup>
            <span className="px-3 pt-1 pb-2 text-xs text-mute">
              The states keep their colors, so a verdict reads the same whatever the accent is.
            </span>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
