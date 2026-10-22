import { timestampDate } from "@bufbuild/protobuf/wkt"
import { ConnectError } from "@connectrpc/connect"
import { createConnectQueryKey, skipToken, useMutation, useQuery } from "@connectrpc/connect-query"
import {
  Button,
  Card,
  CommandBlock,
  ConfirmDialog,
  DialogDescription,
  EmptyState,
  Fingerprint,
  Pill,
  StatusGlyph,
} from "@ptah/console-ui"
import { useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { useState, type FormEvent } from "react"

import {
  connectionName,
  connectionTransport,
  reachable,
  useConnections,
  useConsoleTransport,
} from "../api/cluster"
import { useSession } from "../api/resources"
import {
  ChannelState,
  ConnectionProfile,
  ConnectionService,
  type Connection,
  type Enrollment,
} from "../gen/ptah/console/v1/connections_pb"
import { ScopeService } from "../gen/ptah/console/v1/resources_pb"
import { summarize } from "../model/connection"
import { BackButton, Hero, Loading, ReadError, useNow } from "./common"

/** The roles an enrollment grants on the target, as the agent's chart names them. */
export const enrollmentRoles = [
  "viewer",
  "plan-reader",
  "desired-state-author",
  "diagnostic-reader",
  "approver",
] as const

/** What the person's own identity finds on a connection's cluster, when it is reached. */
function useConnectionScope(connection: Connection) {
  const console = useConsoleTransport()
  const transport =
    connection.channel === ChannelState.IN_CLUSTER
      ? console
      : connectionTransport(console, connection.connectionUid)
  const query = useQuery(
    ScopeService.method.listNamespaces,
    reachable(connection) ? {} : skipToken,
    {
      transport,
    }
  )
  return { data: query.data, error: query.error ? ConnectError.from(query.error).rawMessage : "" }
}

function useRegistryRefresh() {
  const queryClient = useQueryClient()
  return () =>
    queryClient.invalidateQueries({
      queryKey: createConnectQueryKey({ schema: ConnectionService, cardinality: undefined }),
    })
}

/** The Admin role: the connections, the enrollments under way, and adding one. */
export function Connections() {
  const registry = useConnections()
  const transport = useConsoleTransport()
  const enrollments = useQuery(
    ConnectionService.method.listEnrollments,
    registry.manage ? {} : skipToken,
    {
      transport,
    }
  )
  const now = useNow(30_000)
  const attention = registry.connections.filter(
    (connection) => !reachable(connection) || connection.rotationDue
  )
  return (
    <>
      <Hero
        eyebrow="Admin"
        count={attention.length}
        label={attention.length === 1 ? "connection needs attention" : "connections need attention"}
        sub="Connections, enrollments, credentials"
        capability={
          registry.manage
            ? "consoleclusters, consoleclusterenrollments"
            : "get on the connections you were given"
        }
      />
      {registry.manage ? (
        <Link
          to="/connections/new"
          className="inline-flex min-h-11 items-center self-end rounded-xl bg-pri px-4 text-sm font-semibold text-pri-fg"
        >
          Add cluster
        </Link>
      ) : null}
      {(enrollments.data?.enrollments.length ?? 0) > 0 ? (
        <Card label="Enrollments under way">
          <ul className="m-0 flex list-none flex-col gap-3 p-0">
            {enrollments.data?.enrollments.map((enrollment) => (
              <PendingEnrollment key={enrollment.connectionUid} enrollment={enrollment} now={now} />
            ))}
          </ul>
        </Card>
      ) : null}
      {registry.isPending ? (
        <Loading what="the connections" />
      ) : registry.error ? (
        <ReadError error={registry.error} what="the connections" />
      ) : registry.connections.length === 0 ? (
        <EmptyState tone="off" title="No connection is visible to you">
          The registry has connections you were not given; an administrator grants them by name.
        </EmptyState>
      ) : (
        <ul
          aria-label="Connections"
          className="m-0 flex list-none flex-col overflow-hidden rounded-[18px] border border-line bg-card p-0 shadow-card"
        >
          {registry.connections.map((connection) => (
            <ConnectionRow key={connection.connectionUid} connection={connection} now={now} />
          ))}
        </ul>
      )}
    </>
  )
}

function ConnectionRow({ connection, now }: { connection: Connection; now: Date }) {
  const scope = useConnectionScope(connection)
  const summary = summarize(connection, scope.data, scope.error, now)
  return (
    <li className="grid grid-cols-[26px_minmax(0,1fr)_auto] items-center gap-3 border-b border-line2 px-4 py-3.5 last:border-b-0 hover:bg-hover">
      <StatusGlyph tone={summary.tone} size={24} label={summary.label} />
      <span className="flex min-w-0 flex-col gap-1.5">
        <Link
          to="/connections/$name"
          params={{ name: connection.name }}
          className="text-sm font-semibold break-words text-fg"
        >
          {connectionName(connection)}{" "}
          <span className="font-normal text-fg2">· {summary.label}</span>
        </Link>
        <span className="flex flex-wrap gap-1.5">
          {summary.dimensions.map((dimension) => (
            <span key={dimension.name} className="inline-flex items-center gap-1 text-xs text-fg2">
              <StatusGlyph tone={dimension.tone} size={20} label={dimension.text} />
              {dimension.name}
            </span>
          ))}
        </span>
      </span>
      <span aria-hidden="true" className="font-mono text-base font-semibold text-mute">
        ›
      </span>
    </li>
  )
}

function PendingEnrollment({ enrollment, now }: { enrollment: Enrollment; now: Date }) {
  const transport = useConsoleTransport()
  const refresh = useRegistryRefresh()
  const activate = useMutation(ConnectionService.method.activateEnrollment, {
    transport,
    onSuccess: () => void refresh(),
  })
  const expired = enrollment.expiresAt && timestampDate(enrollment.expiresAt) < now
  const state = enrollment.agentConnected
    ? { tone: "ok" as const, text: "The agent is connected. Activate the connection to use it." }
    : enrollment.completion
      ? {
          tone: "unk" as const,
          text: "The helper completed it on the target. Waiting for the agent to connect.",
        }
      : expired
        ? {
            tone: "bad" as const,
            text: "The bootstrap secret expired before the helper ran. Start a new enrollment.",
          }
        : {
            tone: "unk" as const,
            text: `Waiting for the helper on the target. The secret expires at ${enrollment.expiresAt ? timestampDate(enrollment.expiresAt).toLocaleTimeString() : "soon"}.`,
          }
  return (
    <li className="flex flex-col gap-2">
      <span className="flex items-center gap-2 text-sm font-semibold">
        <StatusGlyph tone={state.tone} size={20} label={state.text} />
        {enrollment.clusterName}
      </span>
      <span className="text-[13px] text-fg2">{state.text}</span>
      <Fingerprint value={enrollment.fingerprint} />
      {enrollment.completion ? (
        <span className="font-mono text-xs text-mute">
          {enrollment.completion.targetServer} · CA{" "}
          {enrollment.completion.targetCaSha256.slice(0, 19)}…
        </span>
      ) : null}
      {enrollment.agentConnected ? (
        <Button
          className="self-start"
          disabled={activate.isPending}
          onClick={() => activate.mutate({ clusterName: enrollment.clusterName })}
        >
          Activate {enrollment.clusterName}
        </Button>
      ) : null}
      {activate.error ? (
        <p role="alert" className="m-0 text-sm text-red">
          {ConnectError.from(activate.error).rawMessage}
        </p>
      ) : null}
    </li>
  )
}

type Action = "disable" | "enable" | "quarantine" | "remove" | "credential"

/** One connection: what the console sees of it, and what an administrator may do. */
export function ConnectionDetail({ name }: { name: string }) {
  const registry = useConnections()
  const session = useSession()
  const now = useNow(30_000)
  const [removed, setRemoved] = useState<string[] | null>(null)
  const connection = registry.connections.find((candidate) => candidate.name === name)
  if (removed) {
    return (
      <>
        <BackButton />
        <Card label="Removed">
          <p className="m-0 text-sm text-fg">
            {name} is removed from the console, and its credential is refused. The cleanup on the
            target is pending: the console cannot see it done.
          </p>
          <CommandBlock label="On the target" command={removed.join("\n")} />
        </Card>
      </>
    )
  }
  if (registry.isPending) {
    return <Loading what="the connection" />
  }
  if (!connection) {
    return (
      <>
        <BackButton />
        <EmptyState tone="off" title={`No connection named ${name} is visible to you`} />
      </>
    )
  }
  return (
    <>
      <BackButton />
      <ConnectionFacts connection={connection} now={now} />
      {registry.manage && connection.profile === ConnectionProfile.AGENT ? (
        <ConnectionActions
          connection={connection}
          as={session.data?.kubernetesUsername ?? ""}
          onRemoved={setRemoved}
        />
      ) : null}
    </>
  )
}

function ConnectionFacts({ connection, now }: { connection: Connection; now: Date }) {
  const scope = useConnectionScope(connection)
  const summary = summarize(connection, scope.data, scope.error, now)
  return (
    <>
      <div className="flex flex-col gap-1.5">
        <span className="font-mono text-xs text-mute">
          connection {connection.connectionUid}
          {connection.generation > 0n ? ` · generation ${connection.generation}` : ""}
        </span>
        <h1 className="m-0 flex items-center gap-2.5 font-display text-2xl font-semibold">
          <StatusGlyph tone={summary.tone} size={28} label={summary.label} />
          {connectionName(connection)}
        </h1>
        <span className="text-sm text-fg2">
          {summary.label}
          {connection.gitops ? (
            <>
              {" "}
              · <Pill>GitOps</Pill> the repository owns it; the console changes nothing of it
            </>
          ) : null}
        </span>
      </div>
      <Card label="What the console sees">
        <ul className="m-0 flex list-none flex-col gap-2.5 p-0">
          {summary.dimensions.map((dimension) => (
            <li
              key={dimension.name}
              className="grid grid-cols-[24px_120px_minmax(0,1fr)] items-center gap-2.5 text-sm"
            >
              <StatusGlyph tone={dimension.tone} size={22} label={dimension.tone} />
              <b className="font-semibold">{dimension.name}</b>
              <span className="text-fg2">{dimension.text}</span>
            </li>
          ))}
        </ul>
        <p className="m-0 mt-3 text-xs text-mute">
          {connection.namespaces.join(", ")}
          {connection.targetServer ? ` · ${connection.targetServer}` : ""}
          {connection.credentialExpiresAt
            ? ` · credential expires ${timestampDate(connection.credentialExpiresAt).toLocaleDateString()}`
            : ""}
        </p>
      </Card>
      {summary.todo ? (
        <Card label="What to do">
          <p className="m-0 text-sm text-fg">{summary.todo}</p>
          {summary.fix ? <CommandBlock command={summary.fix} /> : null}
        </Card>
      ) : null}
    </>
  )
}

function ConnectionActions({
  connection,
  as,
  onRemoved,
}: {
  connection: Connection
  as: string
  onRemoved: (cleanup: string[]) => void
}) {
  const transport = useConsoleTransport()
  const refresh = useRegistryRefresh()
  const [action, setAction] = useState<Action | null>(null)
  const [reason, setReason] = useState("")
  const [credential, setCredential] = useState<{ value: string; generation: bigint } | null>(null)
  const done = () => {
    setAction(null)
    setReason("")
    void refresh()
  }
  const enable = useMutation(ConnectionService.method.setConnectionEnabled, {
    transport,
    onSuccess: done,
  })
  const quarantine = useMutation(ConnectionService.method.quarantineConnection, {
    transport,
    onSuccess: done,
  })
  const remove = useMutation(ConnectionService.method.removeConnection, {
    transport,
    onSuccess: (response) => {
      done()
      onRemoved(response.cleanupSteps)
    },
  })
  const issue = useMutation(ConnectionService.method.issueChannelCredential, {
    transport,
    onSuccess: (response) => {
      done()
      setCredential({ value: response.credential, generation: response.generation })
    },
  })
  const busy = enable.isPending || quarantine.isPending || remove.isPending || issue.isPending
  const failure = [enable, quarantine, remove, issue].find((mutation) => mutation.error)?.error
  const quarantined = connection.channel === ChannelState.QUARANTINED
  const name = connection.name
  const titles: Record<Action, string> = {
    disable: `Disable ${name}`,
    enable: `Enable ${name}`,
    quarantine: `Quarantine ${name}`,
    remove: `Remove ${name}`,
    credential: quarantined
      ? `Lift the quarantine of ${name}`
      : `Issue a new credential for ${name}`,
  }
  const explain: Record<Action, string> = {
    disable:
      "Every request to it is refused. Its configuration is kept, and you can enable it again.",
    enable: "Requests reach it again.",
    quarantine:
      "Closes its channel and refuses its credentials, even if its record is applied again. A new credential lifts it.",
    remove:
      "Revokes it first, then removes it from the console and keeps a tombstone. The target is not reached: its cleanup stays pending.",
    credential: quarantined
      ? "Issues a credential at a new generation. The quarantine lifts when the agent runs with it."
      : "Issues a second credential. The old one is accepted until an hour after the new one connects.",
  }
  const send = () => {
    switch (action) {
      case "disable":
      case "enable":
        enable.mutate({ clusterName: name, enabled: action === "enable" })
        break
      case "quarantine":
        quarantine.mutate({ clusterName: name, reason })
        break
      case "remove":
        remove.mutate({ clusterName: name, reason })
        break
      case "credential":
        issue.mutate({ clusterName: name })
        break
    }
  }
  return (
    <Card label="Actions">
      <div className="flex flex-wrap gap-2">
        <Button onClick={() => setAction(connection.enabled ? "disable" : "enable")}>
          {connection.enabled ? "Disable" : "Enable"}
        </Button>
        <Button onClick={() => setAction("credential")}>
          {quarantined ? "Lift the quarantine…" : "Issue a new credential…"}
        </Button>
        {quarantined ? null : (
          <Button variant="attention" onClick={() => setAction("quarantine")}>
            Quarantine…
          </Button>
        )}
        <Button variant="attention" onClick={() => setAction("remove")}>
          Remove…
        </Button>
      </div>
      <p className="m-0 mt-2 text-xs text-mute">
        None of these stops the operator, its plans, approvals or running operations.
      </p>
      {credential ? (
        <div className="mt-3 flex flex-col gap-2">
          <p className="m-0 text-sm text-fg">
            The new credential, shown this once. Write it into the agent&apos;s Secret on the target
            {credential.generation !== connection.generation
              ? `, with generation ${credential.generation}`
              : ""}
            :
          </p>
          <CommandBlock
            label="On the target"
            command={[
              `kubectl --namespace ptah-console-agent create secret generic ptah-console-agent-credential \\`,
              `  --from-literal=credential=${credential.value} --dry-run=client --output yaml | kubectl apply --filename -`,
              ...(credential.generation !== connection.generation
                ? [
                    `helm upgrade ptah-console-agent oci://ghcr.io/stokaro/charts/ptah-console-agent --namespace ptah-console-agent --reuse-values \\`,
                    `  --set connection.generation=${credential.generation}`,
                  ]
                : []),
            ].join("\n")}
          />
        </div>
      ) : null}
      <ConfirmDialog
        open={action !== null}
        onOpenChange={(open) => (open ? null : setAction(null))}
        title={action ? titles[action] : ""}
        titleTone={action === "quarantine" || action === "remove" ? "red" : "fg"}
        where={name}
        as={as}
        busy={busy}
      >
        <DialogDescription className="m-0 text-sm text-fg2">
          {action ? explain[action] : ""}
        </DialogDescription>
        {action === "quarantine" || action === "remove" ? (
          <label className="flex flex-col gap-1 text-sm">
            Reason, for the record
            <input
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              maxLength={256}
              className="h-10 rounded-[10px] border border-line bg-bg px-3 text-sm text-fg"
            />
          </label>
        ) : null}
        {failure ? (
          <p role="alert" className="m-0 text-sm text-red">
            {ConnectError.from(failure).rawMessage}
          </p>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button disabled={busy} onClick={() => setAction(null)}>
            Cancel
          </Button>
          <Button variant="primary" disabled={busy} onClick={send}>
            {action ? titles[action].split(" ")[0] : ""}
          </Button>
        </div>
      </ConfirmDialog>
    </Card>
  )
}

interface AccountRow {
  id: string
  roles: string[]
}

/** Starts an enrollment: the console's side of adding a cluster. */
export function AddCluster() {
  const transport = useConsoleTransport()
  const refresh = useRegistryRefresh()
  const navigate = useNavigate()
  const [name, setName] = useState("")
  const [namespaces, setNamespaces] = useState("")
  const [accounts, setAccounts] = useState<AccountRow[]>([{ id: "", roles: ["viewer"] }])
  const [approver, setApprover] = useState("ptah-operator-ptah-operator-approver")
  const [matched, setMatched] = useState(false)
  const create = useMutation(ConnectionService.method.createEnrollment, {
    transport,
    onSuccess: () => void refresh(),
  })
  const created = create.data
  const submit = (event: FormEvent) => {
    event.preventDefault()
    create.mutate({
      clusterName: name.trim(),
      namespaces: namespaces.split(/[\s,]+/).filter(Boolean),
      accounts: accounts
        .filter((row) => row.id.trim())
        .map((row) => ({ id: row.id.trim(), roles: row.roles })),
      approverClusterRole: approver.trim(),
    })
  }
  const download = () => {
    if (!created) {
      return
    }
    const url = URL.createObjectURL(
      new Blob([created.enrollmentFile as Uint8Array<ArrayBuffer>], { type: "application/yaml" })
    )
    const anchor = document.createElement("a")
    anchor.href = url
    anchor.download = `${name}.enroll`
    anchor.click()
    URL.revokeObjectURL(url)
  }
  return (
    <>
      <BackButton />
      <div className="flex flex-col gap-1.5">
        <span className="text-xs font-semibold tracking-wide text-mute uppercase">
          Add cluster · {created ? (matched ? "3" : "2") : "1"} of 3
        </span>
        <h1 className="m-0 font-display text-2xl font-semibold">Connect a cluster</h1>
        <span className="text-sm text-fg2">
          This adds the connection only. Who may do what inside the cluster stays the target
          administrator&apos;s to grant.
        </span>
      </div>
      {created ? (
        <>
          <Card label="Run this on the target">
            <Button onClick={download}>Download {name}.enroll</Button>
            <CommandBlock
              command={`ptah-console cluster enroll --context <its kubeconfig context> --enrollment-file ./${name}.enroll`}
            />
            <p className="m-0 text-xs text-mute">
              The file holds a single-use secret, valid until{" "}
              {created.expiresAt
                ? timestampDate(created.expiresAt).toLocaleTimeString()
                : "ten minutes from now"}
              . This page does not keep it: download it before you leave.
            </p>
          </Card>
          <Card label="Compare the fingerprint">
            <Fingerprint value={created.fingerprint} />
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={matched}
                onChange={(event) => setMatched(event.target.checked)}
              />
              It matches what the helper printed.
            </label>
            <Button
              variant="primary"
              className="self-start"
              disabled={!matched}
              onClick={() => void navigate({ to: "/connections" })}
            >
              Done
            </Button>
          </Card>
        </>
      ) : (
        <form onSubmit={submit} className="flex flex-col gap-4">
          <Card label="The connection">
            <label className="flex flex-col gap-1 text-sm">
              Name
              <input
                required
                value={name}
                onChange={(event) => setName(event.target.value)}
                pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
                className="h-10 rounded-[10px] border border-line bg-bg px-3 font-mono text-sm text-fg"
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              Namespaces it covers, separated by commas
              <input
                required
                value={namespaces}
                onChange={(event) => setNamespaces(event.target.value)}
                className="h-10 rounded-[10px] border border-line bg-bg px-3 font-mono text-sm text-fg"
              />
            </label>
            <label className="flex flex-col gap-1 text-sm">
              The operator&apos;s approver ClusterRole there
              <input
                required
                value={approver}
                onChange={(event) => setApprover(event.target.value)}
                className="h-10 rounded-[10px] border border-line bg-bg px-3 font-mono text-sm text-fg"
              />
            </label>
          </Card>
          <Card label="Accounts and their roles there">
            {accounts.map((row, index) => (
              <fieldset
                key={index}
                className="m-0 flex flex-col gap-2 rounded-xl border border-line p-3"
              >
                <legend className="px-1 text-xs text-mute">Account {index + 1}</legend>
                <label className="flex flex-col gap-1 text-sm">
                  Account ID
                  <input
                    value={row.id}
                    onChange={(event) =>
                      setAccounts(
                        accounts.map((other, at) =>
                          at === index ? { ...other, id: event.target.value } : other
                        )
                      )
                    }
                    className="h-10 rounded-[10px] border border-line bg-bg px-3 font-mono text-sm text-fg"
                  />
                </label>
                <span className="flex flex-wrap gap-3">
                  {enrollmentRoles.map((role) => (
                    <label key={role} className="flex items-center gap-1.5 text-sm">
                      <input
                        type="checkbox"
                        checked={row.roles.includes(role)}
                        onChange={(event) =>
                          setAccounts(
                            accounts.map((other, at) =>
                              at === index
                                ? {
                                    ...other,
                                    roles: event.target.checked
                                      ? [...other.roles, role]
                                      : other.roles.filter((held) => held !== role),
                                  }
                                : other
                            )
                          )
                        }
                      />
                      {role}
                    </label>
                  ))}
                </span>
              </fieldset>
            ))}
            <Button
              type="button"
              className="self-start"
              onClick={() => setAccounts([...accounts, { id: "", roles: ["viewer"] }])}
            >
              Add an account
            </Button>
          </Card>
          {create.error ? (
            <p role="alert" className="m-0 text-sm text-red">
              {ConnectError.from(create.error).rawMessage}
            </p>
          ) : null}
          <Button type="submit" variant="primary" className="self-end" disabled={create.isPending}>
            Create the enrollment
          </Button>
        </form>
      )}
    </>
  )
}
