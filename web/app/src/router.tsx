import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
  type RouterHistory,
} from "@tanstack/react-router"

import { Home, Launch, Login } from "./screens/Auth"
import { Loading } from "./screens/common"
import { RouteError } from "./screens/RouteError"
import { readResourcesSearch } from "./screens/console/search"
import { FocusLayout, Root } from "./screens/Shell"

// Each screen is its own chunk: the first page loads the shell and the one
// screen it shows.
const ApproverQueue = lazyRouteComponent(() => import("./screens/ApproverQueue"), "ApproverQueue")
const PlanReview = lazyRouteComponent(() => import("./screens/PlanReview"), "PlanReview")
const Releases = lazyRouteComponent(() => import("./screens/Releases"), "Releases")
const ReleaseDetail = lazyRouteComponent(() => import("./screens/ReleaseDetail"), "ReleaseDetail")
const Incidents = lazyRouteComponent(() => import("./screens/Incidents"), "Incidents")
const IncidentDetail = lazyRouteComponent(
  () => import("./screens/IncidentDetail"),
  "IncidentDetail"
)
const Decisions = lazyRouteComponent(() => import("./screens/Decisions"), "Decisions")
const DecisionDetail = lazyRouteComponent(
  () => import("./screens/DecisionDetail"),
  "DecisionDetail"
)
const ConsoleLayout = lazyRouteComponent(() => import("./screens/console/Layout"), "ConsoleLayout")
const Resources = lazyRouteComponent(() => import("./screens/console/Resources"), "Resources")

export interface RouterContext {
  launchError?: string
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Root })

// The focused view: its pages share one column and the role tabs.
const focus = createRoute({ getParentRoute: () => rootRoute, id: "_focus", component: FocusRoute })

function FocusRoute() {
  const { launchError } = rootRoute.useRouteContext()
  return <FocusLayout launchError={launchError} />
}

const indexRoute = createRoute({ getParentRoute: () => focus, path: "/", component: Home })
const launch = createRoute({ getParentRoute: () => focus, path: "launch", component: Launch })
const login = createRoute({ getParentRoute: () => focus, path: "login", component: Login })

const approve = createRoute({
  getParentRoute: () => focus,
  path: "approve",
  component: ApproverQueue,
})
const review = createRoute({
  getParentRoute: () => focus,
  path: "approve/$namespace/$kind/$name",
  component: PlanReview,
})
const releases = createRoute({
  getParentRoute: () => focus,
  path: "releases",
  component: Releases,
})
const release = createRoute({
  getParentRoute: () => focus,
  path: "releases/$namespace/$kind/$name",
  component: ReleaseDetail,
})
const incidents = createRoute({
  getParentRoute: () => focus,
  path: "incidents",
  component: Incidents,
})
const incidentDetail = createRoute({
  getParentRoute: () => focus,
  path: "incidents/$namespace/$kind/$name",
  component: IncidentDetail,
})
const decisions = createRoute({
  getParentRoute: () => focus,
  path: "decisions",
  component: Decisions,
})
const decision = createRoute({
  getParentRoute: () => focus,
  path: "decisions/$namespace/$kind/$name",
  component: DecisionDetail,
})

// The full console: every resource, with a sidebar between its pages.
const consoleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "console",
  component: ConsoleLayout,
})
const consoleIndex = createRoute({
  getParentRoute: () => consoleRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/console/resources", replace: true })
  },
})
const consoleResources = createRoute({
  getParentRoute: () => consoleRoute,
  path: "resources",
  validateSearch: readResourcesSearch,
  component: ResourcesRoute,
})

function ResourcesRoute() {
  const search = consoleResources.useSearch()
  const navigate = consoleResources.useNavigate()
  return (
    <Resources
      search={search}
      onSearch={(next) => void navigate({ search: next, replace: true })}
    />
  )
}

export const routeTree = rootRoute.addChildren([
  focus.addChildren([
    indexRoute,
    launch,
    login,
    approve,
    review,
    releases,
    release,
    incidents,
    incidentDetail,
    decisions,
    decision,
  ]),
  consoleRoute.addChildren([consoleIndex, consoleResources]),
])

export function createAppRouter(
  options: { history?: RouterHistory; context?: RouterContext } = {}
) {
  return createRouter({
    routeTree,
    history: options.history,
    context: options.context ?? {},
    scrollRestoration: true,
    defaultPendingComponent: () => <Loading what="the page" />,
    defaultErrorComponent: RouteError,
    defaultPendingMs: 300,
  })
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
