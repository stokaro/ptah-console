import { Menu } from "@base-ui/react/menu"
import { StatusGlyph, type Tone } from "@ptah/console-ui"
import { useNavigate, useRouterState } from "@tanstack/react-router"

import { connectionName, useCluster, useConnections } from "../api/cluster"
import { channelTone } from "./clusters"

/** Where a page goes when the cluster changes: its list, not a resource the other cluster may lack. */
function listOf(pathname: string) {
  if (pathname.startsWith("/console")) {
    return "/console"
  }
  const first = pathname.split("/").filter(Boolean)[0]
  return first ? `/${first}` : "/"
}

/**
 * The cluster a page shows, and the others the person may see, each with its
 * channel and the namespaces it covers. Only a console with more than one
 * connection shows it.
 */
export function ClusterSwitcher() {
  const { connections, thisCluster } = useConnections()
  const { uid, connection } = useCluster()
  const navigate = useNavigate()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  if (connections.length < 2) {
    return null
  }
  const current = connection ? channelTone(connection) : { tone: "unk" as Tone, label: "unknown" }
  return (
    <Menu.Root>
      <Menu.Trigger
        aria-label={`Cluster: ${connection ? connectionName(connection) : "unknown"}, ${current.label}. Choose another`}
        className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-full border border-line bg-card px-2.5 text-xs font-medium text-fg"
      >
        <StatusGlyph tone={current.tone} size={20} label={current.label} />
        <span className="font-mono">{connection ? connectionName(connection) : uid}</span>
        <span aria-hidden="true" className="text-mute">
          ▾
        </span>
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner sideOffset={6} align="start" className="z-40">
          <Menu.Popup className="flex max-h-[60vh] min-w-[280px] flex-col overflow-auto rounded-[14px] border border-line bg-card p-1 shadow-card">
            {connections.map((candidate) => {
              const state = channelTone(candidate)
              return (
                <Menu.Item
                  key={candidate.connectionUid}
                  onClick={() =>
                    void navigate({
                      to: listOf(pathname),
                      search: (previous) => ({
                        ...previous,
                        cluster:
                          candidate.connectionUid === thisCluster
                            ? undefined
                            : candidate.connectionUid,
                      }),
                    })
                  }
                  className="grid cursor-pointer grid-cols-[24px_minmax(0,1fr)] items-center gap-2.5 rounded-[10px] px-2.5 py-2 text-sm text-fg outline-none data-highlighted:bg-hover"
                >
                  <StatusGlyph tone={state.tone} size={20} label={state.label} />
                  <span className="flex min-w-0 flex-col">
                    <span className="font-semibold break-words">
                      {connectionName(candidate)}
                      {candidate.connectionUid === uid ? (
                        <>
                          {" "}
                          <span className="font-normal text-mute">· shown</span>
                        </>
                      ) : null}
                    </span>
                    <span className="text-xs text-mute">
                      {state.label} · {candidate.namespaces.length}{" "}
                      {candidate.namespaces.length === 1 ? "namespace" : "namespaces"}
                    </span>
                  </span>
                </Menu.Item>
              )
            })}
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
