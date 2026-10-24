import { Mode, SignIn } from "../gen/ptah/console/v1/info_pb"

/**
 * Where a session's Kubernetes identity comes from: a kubeconfig, a Console
 * account the hosted console impersonates, or an OpenID Connect token the
 * API server checks itself. Each reaches Kubernetes differently, so the
 * console words each differently.
 */
export type SessionSource = "kubeconfig" | "account" | "oidc"

export function sessionSource(session: { mode: Mode; signIn?: SignIn }): SessionSource {
  if (session.mode === Mode.LOCAL) {
    return "kubeconfig"
  }
  return session.signIn === SignIn.OIDC ? "oidc" : "account"
}

/** The source's short name, beside the identity it names. */
export const sourceName: Record<SessionSource, string> = {
  kubeconfig: "kubeconfig",
  account: "Console account",
  oidc: "OIDC",
}
