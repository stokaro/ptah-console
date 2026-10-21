import { Wordmark } from "@ptah/console-ui"
import { Link, Outlet } from "@tanstack/react-router"

import { useResources, useScope, useSession } from "../../api/resources"
import { BuildFooter } from "../../BuildFooter"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import { LiveBanner, OperatorApiBanner, SessionChips, SignOut, ThemeSwitch } from "../Shell"

/** The full console's pages, in the order the navigation lists them. */
const pages = [{ to: "/console/resources", label: "Resources" }] as const

/**
 * The full console: every resource the session may read, with a sidebar to
 * move between its pages. The focused view stays one link away.
 */
export function ConsoleLayout() {
  const session = useSession()
  const hosted = session.isSuccess && session.data.mode === Mode.HOSTED
  return (
    <div className="flex min-h-screen flex-col md:flex-row">
      {/* One navigation: a strip across the top on a phone, a sidebar on a wider screen. */}
      <aside className="flex flex-wrap items-center gap-3 border-b border-line bg-[color-mix(in_oklab,var(--card)_55%,var(--bg))] px-4 py-2.5 md:sticky md:top-0 md:h-screen md:w-[232px] md:flex-none md:flex-col md:flex-nowrap md:items-stretch md:gap-6 md:border-r md:border-b-0 md:px-3.5 md:py-4">
        <Link to="/console" aria-label="Ptah Console, full console" className="px-1.5 text-fg">
          <Wordmark caption="console" />
        </Link>
        <ConsoleNav />
        <span className="hidden flex-auto md:block" />
        <Link
          to="/"
          className="flex flex-col gap-0.5 rounded-xl border border-line bg-card px-3 py-2 text-[13px] font-semibold text-fg"
        >
          Focused view
          <span className="text-xs font-normal text-mute">by role</span>
        </Link>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="sticky top-0 z-20 flex flex-wrap items-center gap-3 border-b border-line bg-[color-mix(in_oklab,var(--bg)_85%,transparent)] px-4 py-3 backdrop-blur-[14px] md:px-6">
          <SessionChips />
          <NamespacesChip />
          <span className="flex-auto" />
          {hosted ? <SignOut /> : null}
          <ThemeSwitch />
        </header>
        <main className="mx-auto box-border flex w-full max-w-[1400px] flex-1 flex-col gap-4 px-4 pt-[22px] pb-[72px] md:px-6">
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
  return (
    <nav aria-label="Console" className="flex flex-auto gap-1 md:flex-none md:flex-col">
      {pages.map((page) => (
        <Link
          key={page.to}
          to={page.to}
          className="flex min-h-10 items-center gap-2 rounded-[10px] border border-transparent px-3 text-[13.5px] font-medium text-fg2"
          activeProps={{
            className: "border-line! bg-card text-fg! shadow-card",
            "aria-current": "page",
          }}
        >
          <span className="flex-auto">{page.label}</span>
          {loading ? null : (
            <span className="font-mono text-[11px] text-mute">{resources.length}</span>
          )}
        </Link>
      ))}
    </nav>
  )
}

/** The namespaces the pages read, so a short list is not mistaken for a quiet cluster. */
function NamespacesChip() {
  const scope = useScope()
  if (scope.scopes.length === 0) {
    return null
  }
  const readable = scope.readable
  return (
    <span
      title={readable.join(", ")}
      className="inline-flex h-8 items-center gap-1.5 rounded-full border border-line bg-card px-2.5 text-xs"
    >
      <span className="text-mute">namespaces</span>
      <b className="font-mono font-semibold">
        {readable.length === 1 ? readable[0] : `${readable.length} of ${scope.scopes.length}`}
      </b>
    </span>
  )
}
