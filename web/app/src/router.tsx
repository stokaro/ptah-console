import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  type RouterHistory,
} from "@tanstack/react-router"

import { Home, Launch, Login } from "./screens/Auth"
import { Loading } from "./screens/common"
import { RouteError } from "./screens/RouteError"
import { Shell } from "./screens/Shell"

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

export interface RouterContext {
  launchError?: string
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: ShellRoute })

function ShellRoute() {
  const { launchError } = rootRoute.useRouteContext()
  return <Shell launchError={launchError} />
}

const indexRoute = createRoute({ getParentRoute: () => rootRoute, path: "/", component: Home })
const launch = createRoute({ getParentRoute: () => rootRoute, path: "launch", component: Launch })
const login = createRoute({ getParentRoute: () => rootRoute, path: "login", component: Login })

const approve = createRoute({
  getParentRoute: () => rootRoute,
  path: "approve",
  component: ApproverQueue,
})
const review = createRoute({
  getParentRoute: () => rootRoute,
  path: "approve/$namespace/$kind/$name",
  component: PlanReview,
})
const releases = createRoute({
  getParentRoute: () => rootRoute,
  path: "releases",
  component: Releases,
})
const release = createRoute({
  getParentRoute: () => rootRoute,
  path: "releases/$namespace/$kind/$name",
  component: ReleaseDetail,
})
const incidents = createRoute({
  getParentRoute: () => rootRoute,
  path: "incidents",
  component: Incidents,
})
const incidentDetail = createRoute({
  getParentRoute: () => rootRoute,
  path: "incidents/$namespace/$kind/$name",
  component: IncidentDetail,
})
const decisions = createRoute({
  getParentRoute: () => rootRoute,
  path: "decisions",
  component: Decisions,
})
const decision = createRoute({
  getParentRoute: () => rootRoute,
  path: "decisions/$namespace/$kind/$name",
  component: DecisionDetail,
})

export const routeTree = rootRoute.addChildren([
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
