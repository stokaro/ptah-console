import { Code, ConnectError } from "@connectrpc/connect"
import { useMutation } from "@connectrpc/connect-query"
import { Button, EmptyState } from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Navigate, useNavigate } from "@tanstack/react-router"
import { useState, type FormEvent } from "react"

import { canAnywhere, useHosted, useInfo, useScope, useSession } from "../api/resources"
import { SignIn } from "../gen/ptah/console/v1/info_pb"
import { SessionService } from "../gen/ptah/console/v1/session_pb"
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
  // A query that failed keeps its last data, so data alone does not say
  // there is a session: after a restart it is the session that ended.
  if (session.isSuccess) {
    return <Navigate to="/" replace />
  }
  return (
    <EmptyState tone="warn" title="This page is not signed in">
      Open the link ptah-console printed in your terminal; it works once, for one minute. Press
      Enter there for a new one.
    </EmptyState>
  )
}

/** Why a sign-in with the provider came back without a session, as the console names it. */
export const providerRefusals = [
  "throttled",
  "busy",
  "provider",
  "state",
  "token",
  "refused",
  "unavailable",
  "full",
] as const

export type ProviderRefusal = (typeof providerRefusals)[number]

export interface LoginProps {
  /** Why the last sign-in with the provider did not end in a session. */
  refused?: ProviderRefusal
}

/** How to sign in, for the mode this console runs in. */
export function Login({ refused }: LoginProps) {
  const session = useSession()
  const hosted = useHosted()
  const info = useInfo()
  // A query that failed keeps its last data, so data alone does not say
  // there is a session: after a restart it is the session that ended.
  if (session.isSuccess) {
    return <Navigate to="/" replace />
  }
  if (hosted === undefined) {
    return <Loading what="how this console signs in" />
  }
  if (info.data?.signIn === SignIn.OIDC) {
    return <ProviderSignIn provider={info.data.signInProvider} refused={refused} />
  }
  if (hosted) {
    return <SignInForm />
  }
  return (
    <EmptyState tone="off" title="Sign in">
      ptah-console local signs you in with the link it prints in your terminal, as the identity in
      your kubeconfig; there is no password. Run it, and open that link.
    </EmptyState>
  )
}

function refusalText(refused: ProviderRefusal, provider: string) {
  switch (refused) {
    case "throttled":
      return "Too many sign-ins were started from here. Wait a minute and try again."
    case "busy":
      return "The console has too many sign-ins in progress. Try again in a minute."
    case "provider":
      return `${provider} did not sign you in.`
    case "state":
      return "That sign-in was started in another window, or it took longer than ten minutes. Start again."
    case "token":
      return `${provider}'s answer did not check out, so nobody was signed in.`
    case "refused":
      return `The API server does not accept ${provider}'s token for this console. It has to trust the issuer, with the console's client ID as an audience; ask the cluster's administrator.`
    case "unavailable":
      return "The API server did not answer, so the console could not learn who you are. Try again."
    case "full":
      return "Too many sessions are open on this console. Try again later."
  }
}

/**
 * The sign-in of a hosted console whose people sign in with an OpenID
 * Connect provider. The button is a plain link: the browser goes to the
 * console's own page, which sends it on to the provider.
 */
function ProviderSignIn({ provider, refused }: { provider: string; refused?: ProviderRefusal }) {
  return (
    <section
      aria-labelledby="sign-in-title"
      className="mx-auto flex w-full max-w-[380px] flex-col gap-4 rounded-2xl border border-line bg-card p-6 shadow-card"
    >
      <h1 id="sign-in-title" className="m-0 font-display text-xl font-semibold">
        Sign in
      </h1>
      <p className="m-0 text-sm text-mute">
        With {provider}. What you can see and do is what the API server lets the identity it maps
        your sign-in to do.
      </p>
      {refused ? (
        <p role="alert" className="m-0 text-sm text-red">
          {refusalText(refused, provider)}
        </p>
      ) : null}
      <a
        href="/auth/oidc/login"
        className="inline-flex min-h-11 items-center justify-center rounded-xl bg-pri px-4 text-sm font-semibold text-onpri"
      >
        Sign in with {provider}
      </a>
    </section>
  )
}

/** Why a sign-in did not work, without saying whether the login exists. */
function signInProblem(error: Error) {
  switch (ConnectError.from(error).code) {
    case Code.Unauthenticated:
      return "The login or the password is wrong."
    case Code.ResourceExhausted:
      return "Too many sign-in attempts. Wait a minute and try again."
    default:
      return `Signing in did not work: ${ConnectError.from(error).rawMessage}`
  }
}

/** The sign-in of a hosted console: a Console account's login and password. */
function SignInForm() {
  const [login, setLogin] = useState("")
  const [password, setPassword] = useState("")
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const signIn = useMutation(SessionService.method.login, {
    onSuccess: async () => {
      setPassword("")
      await queryClient.resetQueries()
      await navigate({ to: "/", replace: true })
    },
    onError: () => setPassword(""),
  })
  const submit = (event: FormEvent) => {
    event.preventDefault()
    signIn.mutate({ login, password })
  }
  const field =
    "min-h-11 rounded-xl border border-line bg-card px-3 text-sm text-fg focus-visible:outline-2 focus-visible:outline-pri"
  return (
    <form
      aria-labelledby="sign-in-title"
      onSubmit={submit}
      className="mx-auto flex w-full max-w-[380px] flex-col gap-4 rounded-2xl border border-line bg-card p-6 shadow-card"
    >
      <h1 id="sign-in-title" className="m-0 font-display text-xl font-semibold">
        Sign in
      </h1>
      <p className="m-0 text-sm text-mute">
        With your Console account. What you can see and do is what its Kubernetes identity may.
      </p>
      <label className="flex flex-col gap-1.5 text-sm font-medium">
        Login
        <input
          name="login"
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          required
          value={login}
          onChange={(event) => setLogin(event.target.value)}
          className={field}
        />
      </label>
      <label className="flex flex-col gap-1.5 text-sm font-medium">
        Password
        <input
          name="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          className={field}
        />
      </label>
      {signIn.error ? (
        <p role="alert" className="m-0 rounded-xl bg-redbg px-3 py-2 text-sm text-red">
          {signInProblem(signIn.error)}
        </p>
      ) : null}
      <Button type="submit" variant="primary" disabled={signIn.isPending}>
        {signIn.isPending ? "Signing in…" : "Sign in"}
      </Button>
    </form>
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
