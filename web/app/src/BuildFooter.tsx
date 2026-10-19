import { useQuery } from "@connectrpc/connect-query"

import { InfoService } from "./gen/ptah/console/v1/info_pb"

/** The console build, as the server reports it. */
export function BuildFooter() {
  const info = useQuery(InfoService.method.getInfo, {})
  if (info.isError) {
    return <footer className="px-5 py-3 text-xs text-red">The console API is not reachable.</footer>
  }
  if (!info.data) {
    return <footer className="px-5 py-3 text-xs text-mute">Connecting…</footer>
  }
  return (
    <footer className="px-5 py-3 text-xs text-mute">
      ptah-console {info.data.version}
      {info.data.readOnly ? " · read-only" : ""}
    </footer>
  )
}
