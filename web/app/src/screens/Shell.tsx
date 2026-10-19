import { Code, ConnectError } from "@connectrpc/connect"
import { EmptyState, Wordmark } from "@ptah/console-ui"
import { Link, Outlet, useRouterState } from "@tanstack/react-router"

import { LiveUpdates, useLive } from "../api/live"
import { useScope, useSession } from "../api/resources"
import { BuildFooter } from "../BuildFooter"
import { Mode } from "../gen/ptah/console/v1/info_pb"
import { useTheme, type ThemeChoice } from "../theme"

const roles = [
  { to: "/approve", label: "Approver" },
  { to: "/releases", label: "Developer" },
  { to: "/incidents", label: "On call" },
  { to: "/decisions", label: "Auditor" },
] as const

const themes: { value: ThemeChoice; label: string }[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
]

export interface ShellProps {
  /** Why the launch link could not sign this browser in, if it could not. */
  launchError?: string
}

export function Shell({ launchError }: ShellProps) {
  const session = useSession()
  const scope = useScope()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const onList = pathname.split("/").filter(Boolean).length <= 1
  const signedOut = session.error && ConnectError.from(session.error).code === Code.Unauthenticated

  return (
    <div className="flex min-h-screen flex-col">
      <Header />
      {launchError ? (
        <p role="alert" className="m-0 border-b border-line bg-redbg px-5 py-3 text-sm text-red">
          {launchError}
        </p>
      ) : null}
      <main className="mx-auto box-border flex w-full max-w-[820px] flex-1 flex-col gap-4 px-5 pt-[22px] pb-[72px]">
        {signedOut ? (
          <EmptyState tone="warn" title="This page is not signed in">
            Open the link ptah-console printed in your terminal. Press Enter there for a new one.
          </EmptyState>
        ) : (
          <LiveUpdates namespaces={scope.readable}>
            <LiveBanner />
            {onList ? <RoleNav /> : null}
            <Outlet />
          </LiveUpdates>
        )}
      </main>
      <BuildFooter />
    </div>
  )
}

function Header() {
  const session = useSession()
  const [theme, setTheme] = useTheme()
  const identity = session.data
  return (
    <header className="sticky top-0 z-20 flex flex-wrap items-center gap-3 border-b border-line bg-[color-mix(in_oklab,var(--bg)_85%,transparent)] px-5 py-3 backdrop-blur-[14px]">
      <Link to="/" aria-label="Ptah Console, home" className="text-fg">
        <Wordmark caption="focus" />
      </Link>
      {identity ? (
        <span className="inline-flex h-8 items-center gap-1.5 rounded-full border border-line bg-card px-2.5 text-xs">
          <span className="text-mute">as</span>
          <b className="font-mono font-semibold">{identity.kubernetesUsername}</b>
          <span className="text-mute">
            · {identity.mode === Mode.LOCAL ? "kubeconfig" : "Console-managed"} {identity.context}
          </span>
        </span>
      ) : null}
      {identity?.readOnly ? (
        <span className="rounded-full bg-greybg px-2.5 py-1 text-xs text-fg2">Read-only</span>
      ) : null}
      {identity?.kubeconfigImpersonates ? (
        <span className="text-xs text-amber">
          the kubeconfig impersonates {identity.kubeconfigImpersonates}
        </span>
      ) : null}
      {identity?.insecureTls ? (
        <span className="text-xs text-red">the API server&apos;s certificate is not verified</span>
      ) : null}
      <span className="flex-auto" />
      <div
        role="radiogroup"
        aria-label="Theme"
        className="flex gap-0.5 rounded-[11px] border border-line p-[3px]"
      >
        {themes.map((option) => (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={theme === option.value}
            onClick={() => setTheme(option.value)}
            className={`h-[30px] cursor-pointer rounded-lg border-0 px-2.5 text-xs font-medium ${theme === option.value ? "bg-pribg text-pri" : "bg-transparent text-mute"}`}
          >
            {option.label}
          </button>
        ))}
      </div>
    </header>
  )
}

function RoleNav() {
  return (
    <nav
      aria-label="Role"
      className="flex gap-1 overflow-x-auto rounded-2xl border border-line bg-[color-mix(in_oklab,var(--card)_60%,transparent)] p-1"
    >
      {roles.map((role) => (
        <Link
          key={role.to}
          to={role.to}
          className="flex min-h-11 flex-[1_0_auto] items-center justify-center rounded-xl border border-transparent px-3.5 text-[13.5px] font-semibold whitespace-nowrap text-fg2"
          activeProps={{
            className: "border-line! bg-card text-fg! shadow-card",
            "aria-current": "page",
          }}
        >
          {role.label}
        </Link>
      ))}
    </nav>
  )
}

function LiveBanner() {
  const live = useLive()
  if (live.status === "ended") {
    return (
      <p
        role="alert"
        className="m-0 rounded-xl border border-line bg-amberbg px-4 py-3 text-sm text-fg"
      >
        The session ended. Open a new link from ptah-console in your terminal.
      </p>
    )
  }
  if (live.status !== "lost") {
    return null
  }
  const since = live.lostSince?.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })
  return (
    <p
      role="status"
      className="m-0 rounded-xl border border-line bg-violetbg px-4 py-3 text-sm text-fg"
    >
      Live updates stopped at {since}. What you see may be behind; reconnecting. Every decision is
      checked against the cluster again before it is sent.
    </p>
  )
}
