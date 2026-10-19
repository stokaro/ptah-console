import { EmptyState } from "@ptah/console-ui"
import { Navigate } from "@tanstack/react-router"

import { canAnywhere, useScope, useSession } from "../api/resources"
import { Mode } from "../gen/ptah/console/v1/info_pb"
import { Loading } from "./common"

/**
 * Where the launch link lands and where a page without a session is sent.
 * The link's token was traded for a session before the first render; a
 * signed-in browser goes on to its home.
 */
export function Launch() {
  const session = useSession()
  if (session.isPending) {
    return <Loading what="the session" />
  }
  if (session.data) {
    return <Navigate to="/" replace />
  }
  return (
    <EmptyState tone="warn" title="This page is not signed in">
      Open the link ptah-console printed in your terminal; it works once, for one minute. Press
      Enter there for a new one.
    </EmptyState>
  )
}

/** How to sign in, for the mode this console runs in. */
export function Login() {
  const session = useSession()
  if (session.data?.mode === Mode.LOCAL) {
    return <Navigate to="/" replace />
  }
  return (
    <EmptyState tone="off" title="Sign in">
      ptah-console local signs you in with the link it prints in your terminal, as the identity in
      your kubeconfig; there is no password. Run it, and open that link.
    </EmptyState>
  )
}

/** The first view: the one for what this identity may do. */
export function Home() {
  const scope = useScope()
  if (scope.isPending) {
    return <Loading what="what you may do" />
  }
  const approves =
    canAnywhere(scope.scopes, "approveSchemaPlans") ||
    canAnywhere(scope.scopes, "approveMigrationPlans")
  if (approves) {
    return <Navigate to="/approve" replace />
  }
  if (canAnywhere(scope.scopes, "acknowledgeRuns") || canAnywhere(scope.scopes, "readLogs")) {
    return <Navigate to="/incidents" replace />
  }
  return <Navigate to="/releases" replace />
}
