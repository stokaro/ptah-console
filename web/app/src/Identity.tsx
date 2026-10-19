import { useQuery } from "@connectrpc/connect-query"

import { Mode } from "./gen/ptah/console/v1/info_pb"
import { SessionService } from "./gen/ptah/console/v1/session_pb"

/** Whom the console's requests run as, as the API server reports it. */
export function Identity() {
  const session = useQuery(SessionService.method.getSession, {})
  if (!session.data) {
    return null
  }
  const { kubernetesUsername, context, mode, kubeconfigImpersonates, insecureTls } = session.data
  return (
    <span className="flex flex-wrap items-center gap-2 text-xs">
      <span className="rounded-full border border-slate-200 px-2.5 py-1">
        <span className="text-slate-500">as </span>
        <b className="font-mono font-semibold">{kubernetesUsername}</b>
      </span>
      <span className="text-slate-500">
        {mode === Mode.LOCAL ? "kubeconfig" : "Console-managed"} · {context}
      </span>
      {kubeconfigImpersonates ? (
        <span className="text-amber-700">the kubeconfig impersonates {kubeconfigImpersonates}</span>
      ) : null}
      {insecureTls ? (
        <span className="text-red-700">the API server&apos;s certificate is not verified</span>
      ) : null}
    </span>
  )
}
