import { Code, ConnectError } from "@connectrpc/connect"
import { useMutation } from "@connectrpc/connect-query"
import { Button, Freshness, Pill, Wordmark } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link, Navigate, Outlet, useRouterState } from "@tanstack/react-router"
import { Suspense } from "react"

import { useConnections } from "../api/cluster"
import { LiveUpdates, useLive } from "../api/live"
import { useHosted, useScope, useSession } from "../api/resources"
import { BuildFooter } from "../BuildFooter"
import { Mode } from "../gen/ptah/console/v1/info_pb"
import { SessionService } from "../gen/ptah/console/v1/session_pb"
import { useTheme, type ThemeChoice } from "../theme"
import { ClusterBanner, ClusterSwitcher } from "./clusters"
import { useNow } from "./common"

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

/**
 * What every page sits in: a signed-out browser goes to sign in, and the live
 * stream follows the namespaces the session may read. The focused view and
 * the full console are two layouts under it.
 */
export function Root() {
  const session = useSession()
  const scope = useScope()
  const hosted = useHosted()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const signedOut = session.error && ConnectError.from(session.error).code === Code.Unauthenticated

  if (signedOut && !onAuthPage(pathname) && hosted !== undefined) {
    return <Navigate to={hosted ? "/login" : "/launch"} replace />
  }
  return (
    <LiveUpdates namespaces={signedOut ? [] : scope.readable}>
      <Outlet />
    </LiveUpdates>
  )
}

function onAuthPage(pathname: string) {
  return pathname === "/launch" || pathname === "/login"
}

export interface FocusLayoutProps {
  /** Why the launch link could not sign this browser in, if it could not. */
  launchError?: string
}

/** The focused view: one column, a page per role. */
export function FocusLayout({ launchError }: FocusLayoutProps) {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const onList = pathname.split("/").filter(Boolean).length <= 1
  return (
    <div className="flex min-h-screen flex-col">
      <Header />
      {launchError ? (
        <p role="alert" className="m-0 border-b border-line bg-redbg px-5 py-3 text-sm text-red">
          {launchError}
        </p>
      ) : null}
      <main className="mx-auto box-border flex w-full max-w-[820px] flex-1 flex-col gap-4 px-5 pt-[22px] pb-[72px]">
        {onAuthPage(pathname) ? (
          <Outlet />
        ) : (
          <>
            <ClusterBanner />
            <OperatorApiBanner />
            <LiveBanner />
            {onList ? <RoleNav /> : null}
            <Outlet />
          </>
        )}
      </main>
      <BuildFooter />
    </div>
  )
}

/** Says when the cluster does not serve the operator's API, or not all of it. */
export function OperatorApiBanner() {
  const scope = useScope()
  const answer = scope.data?.operatorApi
  if (!answer || (answer.served && answer.missing.length === 0)) {
    return null
  }
  return (
    <p
      role="alert"
      className="m-0 rounded-xl border border-line bg-redbg px-4 py-3 text-sm text-fg"
    >
      {answer.problem}
      {answer.missing.length > 0 ? (
        <>
          {" "}
          Not served: <code className="font-mono">{answer.missing.join(", ")}</code>.
        </>
      ) : null}
    </p>
  )
}

function Header() {
  const session = useSession()
  // A failed refresh keeps the last session's data; it names nobody now.
  const identity = session.isSuccess ? session.data : undefined
  return (
    <header className="sticky top-0 z-20 flex flex-wrap items-center gap-3 border-b border-line bg-[color-mix(in_oklab,var(--bg)_85%,transparent)] px-5 py-3 backdrop-blur-[14px]">
      <Link to="/" aria-label="Ptah Console, home" className="text-fg">
        <Wordmark caption="focus" />
      </Link>
      <Suspense fallback={null}>
        <ClusterSwitcher />
      </Suspense>
      <SessionChips />
      <span className="flex-auto" />
      {identity ? (
        <Link
          to="/console"
          className="inline-flex h-8 items-center rounded-full border border-line bg-card px-3 text-xs font-medium text-fg"
        >
          Full console
        </Link>
      ) : null}
      {identity?.mode === Mode.HOSTED ? <SignOut /> : null}
      <ThemeSwitch />
    </header>
  )
}

/** Who the console acts as, and what limits that: the header's left side. */
export function SessionChips() {
  const session = useSession()
  // A failed refresh keeps the last session's data; it names nobody now.
  const identity = session.isSuccess ? session.data : undefined
  return (
    <>
      {identity ? (
        <span className="inline-flex h-8 items-center gap-1.5 rounded-full border border-line bg-card px-2.5 text-xs">
          {identity.displayName ? <span>{identity.displayName}</span> : null}
          <span className="text-mute">as</span>
          <b className="font-mono font-semibold">{identity.kubernetesUsername}</b>
          <span className="text-mute">
            · {identity.mode === Mode.LOCAL ? "kubeconfig" : "Console-managed"} {identity.context}
          </span>
        </span>
      ) : null}
      {identity?.readOnly ? <Pill>Read-only</Pill> : null}
      <HeaderFreshness />
      {identity?.kubeconfigImpersonates ? (
        <span className="text-xs text-amber">
          the kubeconfig impersonates {identity.kubeconfigImpersonates}
        </span>
      ) : null}
      {identity?.insecureTls ? (
        <span className="text-xs text-red">the API server&apos;s certificate is not verified</span>
      ) : null}
    </>
  )
}

export function ThemeSwitch() {
  const [theme, setTheme] = useTheme()
  return (
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
  )
}

/** Ends the session; the shell then shows the sign-in again. */
export function SignOut() {
  const queryClient = useQueryClient()
  const signOut = useMutation(SessionService.method.logout, {
    onSettled: () => queryClient.resetQueries(),
  })
  return (
    <Button
      className="min-h-8! px-3! text-xs"
      disabled={signOut.isPending}
      onClick={() => signOut.mutate({})}
    >
      Sign out
    </Button>
  )
}

function RoleNav() {
  // Connections are the registry's: the Admin role is offered to who may change it.
  const { manage } = useConnections()
  const shown = manage ? [...roles, { to: "/connections", label: "Admin" } as const] : roles
  return (
    <nav
      aria-label="Role"
      className="flex gap-1 overflow-x-auto rounded-2xl border border-line bg-[color-mix(in_oklab,var(--card)_60%,transparent)] p-1"
    >
      {shown.map((role) => (
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

/** How current the page is, while there is anything to follow. */
function HeaderFreshness() {
  const live = useLive()
  const scope = useScope()
  const now = useNow(5_000)
  if (scope.readable.length === 0) {
    return null
  }
  return <Freshness state={live.status} at={live.lastHeartbeat} now={now} />
}

export function LiveBanner() {
  const live = useLive()
  const hosted = useHosted()
  if (live.status === "ended") {
    return (
      <p
        role="alert"
        className="m-0 rounded-xl border border-line bg-amberbg px-4 py-3 text-sm text-fg"
      >
        {hosted
          ? "The session ended. Sign in again."
          : "The session ended. Open a new link from ptah-console in your terminal."}
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
      Live updates stopped at {since}. What you see may be behind, and decisions wait until the
      console is in touch with the cluster again; it reconnects on its own.
    </p>
  )
}
