import { create, type MessageInitShape } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect"
import { createMemoryHistory } from "@tanstack/react-router"
import { QueryClient } from "@tanstack/react-query"
import { render } from "@testing-library/react"

import { App } from "../App"
import { AppProviders } from "../AppProviders"
import {
  DecisionResultSchema,
  DecisionService,
  type AcknowledgeMigrationRunRequest,
  type ApproveSchemaPlanRequest,
  type Decision,
  type DecisionResult,
  type GetDecisionRequest,
  type SetSuspendedRequest,
  type SetSuspendedResponseSchema,
} from "../gen/ptah/console/v1/decisions_pb"
import {
  DiagnosticsService,
  type GetDiagnosticsResponseSchema,
} from "../gen/ptah/console/v1/diagnostics_pb"
import { connectionHeader } from "../api/cluster"
import {
  ConnectionService,
  type ConnectionSchema,
  type CreateEnrollmentResponseSchema,
  type EnrollmentSchema,
} from "../gen/ptah/console/v1/connections_pb"
import { InfoService, Mode } from "../gen/ptah/console/v1/info_pb"
import {
  Access,
  Answer,
  AttentionKind,
  MigrationSchema,
  MigrationService,
  SchemaPlanSchema,
  SchemaSchema,
  SchemaService,
  ScopeService,
  SqlAccess,
  StageState,
  type CapabilitiesSchema,
  type OperatorApiSchema,
  type ListNamespacesResponseSchema,
  type Migration,
  type MigrationPlan,
  type Schema,
  type SchemaPlan,
} from "../gen/ptah/console/v1/resources_pb"
import { SessionService, type LoginRequest } from "../gen/ptah/console/v1/session_pb"
import { WatchService, type SubscribeResponseSchema } from "../gen/ptah/console/v1/watch_pb"

export const minutesAgo = (minutes: number) =>
  timestampFromDate(new Date(Date.now() - minutes * 60_000))

const allowed: MessageInitShape<typeof CapabilitiesSchema> = {
  listSchemas: Answer.ALLOWED,
  listMigrations: Answer.ALLOWED,
  readPlanSql: Answer.ALLOWED,
  approveSchemaPlans: Answer.ALLOWED,
  approveMigrationPlans: Answer.ALLOWED,
  acknowledgeRuns: Answer.ALLOWED,
  suspendSchemas: Answer.ALLOWED,
  suspendMigrations: Answer.ALLOWED,
  listDecisions: Answer.ALLOWED,
  readJobs: Answer.ALLOWED,
  readPods: Answer.ALLOWED,
  readLogs: Answer.ALLOWED,
  readEvents: Answer.ALLOWED,
}

/** A schema whose current plan waits for an approval. */
export function waitingSchema(change: (schema: Schema) => void = () => {}): Schema {
  const schema = create(SchemaSchema, {
    ref: {
      namespace: "application",
      name: "storefront",
      uid: "uid-schema",
      generation: 3n,
      resourceVersion: "41",
    },
    phase: "AwaitingApproval",
    statusCurrent: true,
    applyPolicy: "OnApproval",
    source: {
      requested: "oci://registry.example/schemas/demo@sha256:5f2ce7ae",
      digest: "sha256:5f2c00000000e7ae",
    },
    currentPlan: {
      name: "storefront-p-8f2c",
      uid: "uid-plan",
      fingerprint: "sha256:9c2400000000000000007b10",
      destructive: true,
      privilegeChanges: ["GRANT"],
      statementCount: 3,
      createdAt: minutesAgo(12),
    },
    stages: [
      { name: "Resolve", state: StageState.DONE },
      { name: "Plan", state: StageState.DONE },
      { name: "Approve", state: StageState.WAITING, detail: "Waits for an approval." },
      { name: "Apply", state: StageState.NOT_APPLICABLE },
      { name: "Verify", state: StageState.NOT_APPLICABLE },
    ],
    attention: [
      {
        kind: AttentionKind.WAITING_FOR_APPROVAL,
        reason: "Waiting for an approver. Nothing has changed in the database yet.",
        nextStep: "Send the approval link to an approver.",
        since: minutesAgo(12),
      },
    ],
    conditions: [
      {
        type: "PlanReady",
        status: "True",
        reason: "Published",
        message: "3 statements",
        lastTransition: minutesAgo(12),
      },
      {
        type: "ApprovalRequired",
        status: "True",
        reason: "AwaitingApproval",
        message: "approve storefront-p-8f2c",
        lastTransition: minutesAgo(12),
      },
    ],
  })
  change(schema)
  return schema
}

/** A migration whose every stage is done: the history is current. */
export function syncedMigration(change: (migration: Migration) => void = () => {}): Migration {
  const migration = create(MigrationSchema, {
    ref: {
      namespace: "application",
      name: "billing",
      uid: "uid-migration",
      generation: 2n,
      resourceVersion: "17",
    },
    phase: "InSync",
    statusCurrent: true,
    applyPolicy: "Always",
    source: {
      requested: "oci://registry.example/migrations/billing@sha256:0a1b2c3d",
      digest: "sha256:0a1b00000000c3d4",
    },
    stages: [
      { name: "Resolve", state: StageState.DONE },
      { name: "Read history", state: StageState.DONE },
      { name: "Plan", state: StageState.DONE },
      { name: "Approve", state: StageState.NOT_APPLICABLE },
      { name: "Apply", state: StageState.DONE },
      { name: "Verify history", state: StageState.DONE },
    ],
    conditions: [
      {
        type: "InSync",
        status: "True",
        reason: "Converged",
        message: "4 migrations applied",
        lastTransition: minutesAgo(40),
      },
    ],
  })
  change(migration)
  return migration
}

export function storedPlan(change: (plan: SchemaPlan) => void = () => {}): SchemaPlan {
  const plan = create(SchemaPlanSchema, {
    namespace: "application",
    schema: "storefront",
    schemaUid: "uid-schema",
    name: "storefront-p-8f2c",
    uid: "uid-plan",
    fingerprint: "sha256:9c2400000000000000007b10",
    contractVersion: 1,
    contractSupported: true,
    destructive: true,
    privilegeChanges: ["GRANT"],
    statementCount: 3,
    statements: [
      {
        index: 0,
        sql: "CREATE TABLE customers (id BIGINT PRIMARY KEY, email TEXT);",
        severity: "safe",
      },
      {
        index: 1,
        sql: "ALTER TABLE orders DROP COLUMN legacy_reference; -- <script>alert(1)</script>",
        severity: "destructive",
        reason: "drops a column",
      },
      {
        index: 2,
        sql: "GRANT SELECT ON customers TO reporting;",
        severity: "warning",
        reason: "grants",
      },
    ],
    chunks: [
      { name: "storefront-p-8f2c-0", index: 0, size: 120, digest: "sha256:aa" },
      { name: "storefront-p-8f2c-1", index: 1, size: 80, digest: "sha256:bb" },
    ],
    artifactDigest: "sha256:5f2c00000000e7ae",
    sqlAccess: SqlAccess.AVAILABLE,
    createdAt: minutesAgo(12),
  })
  change(plan)
  return plan
}

export interface Fixture {
  username?: string
  readOnly?: boolean
  capabilities?: MessageInitShape<typeof CapabilitiesSchema>
  schemas?: Schema[]
  /** The migrations, read again on every request. */
  migrations?: Migration[] | (() => Migration[])
  decisions?: Decision[]
  /** The current plan, read again on every request. */
  plan?: SchemaPlan | (() => SchemaPlan)
  /** A migration's current plan, or the error reading it gives. */
  migrationPlan?: MigrationPlan | ConnectError
  /** Listing schemas is refused; reading one by name is not. */
  listForbidden?: boolean
  approve?: (request: ApproveSchemaPlanRequest) => DecisionResult | Promise<DecisionResult>
  acknowledge?: (request: AcknowledgeMigrationRunRequest) => DecisionResult
  getDecision?: (request: GetDecisionRequest) => Decision
  setSuspended?: (
    request: SetSuspendedRequest
  ) => MessageInitShape<typeof SetSuspendedResponseSchema>
  diagnostics?: MessageInitShape<typeof GetDiagnosticsResponseSchema>
  /** What discovery says; served in full by default. */
  operatorApi?: MessageInitShape<typeof OperatorApiSchema>
  /** Namespaces whose lists stopped short. */
  truncated?: string[]
  /** Namespaces in scope beside application, such as one the session may not read. */
  otherNamespaces?: { name: string; access: Access }[]
  /** The identity may list more namespaces than the console offers. */
  namespacesTruncated?: boolean
  /** The session is answered only once this settles, after everything else. */
  sessionAnswered?: Promise<void>
  /** Called each time the console lists its namespaces. */
  onListNamespaces?: () => void
  /** The cluster's version as discovery says it; v1.36.4 by default. */
  kubernetesVersion?: string
  /** The minors this console release was tested on; 1.35 to 1.37 by default. */
  kubernetesMinors?: string[]
  /** The namespaces each list request named, in order; filled as the console asks. */
  listed?: { method: "listSchemas" | "listMigrations"; namespaces: string[] }[]
  /** The live stream; one heartbeat, then quiet, by default. */
  watch?: (signal: AbortSignal) => AsyncIterable<MessageInitShape<typeof SubscribeResponseSchema>>
  /**
   * A hosted console. The browser starts signed out; login throws to refuse
   * a sign-in, and returning signs the account in.
   */
  hosted?: { login: (request: LoginRequest) => void; signedIn?: boolean }
  /** The registry of a hosted console with external clusters. */
  registry?: RegistryFixture
  /** The server's side of the session; set ended to forget it, as a restart does. */
  server?: { ended: boolean }
}

/** A hosted console's registry, and what its clusters answer. */
export interface RegistryFixture {
  thisCluster: string
  manage: boolean
  connections: MessageInitShape<typeof ConnectionSchema>[]
  enrollments?: MessageInitShape<typeof EnrollmentSchema>[]
  /**
   * What a connection's cluster answers about its namespaces, by connection
   * UID: unavailable for one whose agent is not connected. Unset is the
   * application namespace, readable.
   */
  clusters?: Record<string, "unavailable" | MessageInitShape<typeof ListNamespacesResponseSchema>>
  created?: MessageInitShape<typeof CreateEnrollmentResponseSchema>
  /** The changes the console was asked for, in order. */
  calls: { method: string; request: unknown }[]
  /** The connection header of each request a cluster served. */
  headers: string[]
}

/** A watch that says it is live once and then stays quiet. */
async function* quietWatch(signal: AbortSignal) {
  yield heartbeat()
  await new Promise((resolve) => signal.addEventListener("abort", resolve))
}

export function heartbeat(): MessageInitShape<typeof SubscribeResponseSchema> {
  return { event: { case: "heartbeat", value: { observedAt: timestampFromDate(new Date()) } } }
}

export function result(init: MessageInitShape<typeof DecisionResultSchema>) {
  return create(DecisionResultSchema, {
    name: "ptah-console-0123456789abcdef0123",
    requestId: "r-1",
    ...init,
  })
}

/** The console's API in memory, answering from the fixture. */
export function consoleTransport(fixture: Fixture) {
  const access = (namespace: string) =>
    namespace === "application"
      ? Access.OK
      : (fixture.otherNamespaces?.find((other) => other.name === namespace)?.access ??
        Access.FORBIDDEN)
  const hosted = fixture.hosted
  let signedIn = hosted === undefined || hosted.signedIn === true
  const registry = fixture.registry
  /** The cluster a request names, recorded; throws for one that is not reached. */
  const cluster = (header: Headers) => {
    const uid = header.get(connectionHeader) ?? ""
    registry?.headers.push(uid)
    if (uid && registry?.clusters?.[uid] === "unavailable") {
      throw new ConnectError("the cluster's agent is not connected", Code.Unavailable)
    }
    return uid
  }
  const requireSession = () => {
    if (!signedIn || fixture.server?.ended) {
      throw new ConnectError("sign in first", Code.Unauthenticated)
    }
  }
  return createRouterTransport(({ service }) => {
    service(InfoService, {
      getInfo: () => ({
        version: "v0.1.0",
        commit: "0123456789abcdef0123456789abcdef01234567",
        mode: hosted ? Mode.HOSTED : Mode.LOCAL,
        operatorVersion: "v0.2.5",
        planContractVersion: 1,
        kubernetesMinors: fixture.kubernetesMinors ?? ["1.35", "1.36", "1.37"],
      }),
    })
    service(SessionService, {
      getSession: async () => {
        requireSession()
        await fixture.sessionAnswered
        return hosted
          ? {
              mode: Mode.HOSTED,
              context: "in-cluster",
              kubernetesUsername: fixture.username ?? "ptah-console:prod:alex",
              accountId: "alex",
              displayName: "Alex Morgan",
              readOnly: fixture.readOnly ?? false,
            }
          : {
              mode: Mode.LOCAL,
              context: "kind-ptah",
              kubernetesUsername: fixture.username ?? "alice",
              readOnly: fixture.readOnly ?? false,
            }
      },
      login: (request) => {
        hosted?.login(request)
        signedIn = true
        if (fixture.server) {
          fixture.server.ended = false
        }
        return {}
      },
      logout: () => {
        signedIn = false
        return {}
      },
    })
    if (registry) {
      const record = (method: string, request: unknown) => {
        registry.calls.push({ method, request })
        return {}
      }
      service(ConnectionService, {
        listConnections: () => ({
          connections: registry.connections,
          manage: registry.manage,
          thisCluster: registry.thisCluster,
        }),
        listEnrollments: () => ({ enrollments: registry.enrollments ?? [] }),
        createEnrollment: (request) => {
          record("createEnrollment", request)
          return registry.created ?? {}
        },
        activateEnrollment: (request) => record("activateEnrollment", request),
        setConnectionEnabled: (request) => record("setConnectionEnabled", request),
        quarantineConnection: (request) => {
          record("quarantineConnection", request)
          return { generation: 1n }
        },
        removeConnection: (request) => {
          record("removeConnection", request)
          return {
            connectionUid: "uid",
            cleanupPending: true,
            cleanupSteps: ["helm uninstall ptah-console-agent --namespace ptah-console-agent"],
          }
        },
        issueChannelCredential: (request) => {
          record("issueChannelCredential", request)
          return { credential: "shown-once-credential", generation: 2n }
        },
      })
    }
    service(ScopeService, {
      listNamespaces: (_request, context) => {
        requireSession()
        fixture.onListNamespaces?.()
        const answer = registry?.clusters?.[cluster(context.requestHeader)]
        if (answer && answer !== "unavailable") {
          return answer
        }
        return {
          namespaces: [
            {
              name: "application",
              access: Access.OK,
              capabilities: fixture.capabilities ?? allowed,
            },
            ...(fixture.otherNamespaces ?? []),
          ],
          operatorApi: fixture.operatorApi ?? { served: true },
          truncated: fixture.namespacesTruncated ?? false,
          maxNamespaces: 64,
          kubernetesVersion: fixture.kubernetesVersion ?? "v1.36.4",
        }
      },
    })
    service(SchemaService, {
      listSchemas: (request, context) => {
        cluster(context.requestHeader)
        fixture.listed?.push({ method: "listSchemas", namespaces: [...request.namespaces] })
        if (fixture.listForbidden) {
          throw new ConnectError("may not list ptahschemas", Code.PermissionDenied)
        }
        return {
          // A list answers for the namespaces it names, as the server does.
          schemas: (fixture.schemas ?? []).filter((schema) =>
            request.namespaces.includes(schema.ref?.namespace ?? "")
          ),
          namespaces: request.namespaces.map((namespace) => ({
            namespace,
            access: access(namespace),
            truncated: (fixture.truncated ?? []).includes(namespace),
          })),
        }
      },
      getSchemaPlan: () => ({
        plan: typeof fixture.plan === "function" ? fixture.plan() : (fixture.plan ?? storedPlan()),
      }),
      getSchema: (request) => {
        const found = (fixture.schemas ?? []).find((schema) => schema.ref?.name === request.name)
        if (!found) {
          throw new ConnectError("schema not found", Code.NotFound)
        }
        return { schema: found }
      },
    })
    const migrations = () =>
      typeof fixture.migrations === "function" ? fixture.migrations() : (fixture.migrations ?? [])
    service(MigrationService, {
      listMigrations: (request) => {
        fixture.listed?.push({ method: "listMigrations", namespaces: [...request.namespaces] })
        return {
          migrations: migrations().filter((migration) =>
            request.namespaces.includes(migration.ref?.namespace ?? "")
          ),
        }
      },
      getMigration: (request) => {
        const found = migrations().find((migration) => migration.ref?.name === request.name)
        if (!found) {
          throw new ConnectError("migration not found", Code.NotFound)
        }
        return { migration: found }
      },
      getMigrationPlan: () => {
        if (fixture.migrationPlan instanceof ConnectError) {
          throw fixture.migrationPlan
        }
        return { plan: fixture.migrationPlan }
      },
    })
    service(DecisionService, {
      listDecisions: () => ({ decisions: fixture.decisions ?? [] }),
      getDecision: (request) => ({ decision: fixture.getDecision?.(request) }),
      approveSchemaPlan: async (request) => ({ result: await fixture.approve?.(request) }),
      acknowledgeMigrationRun: (request) => ({ result: fixture.acknowledge?.(request) }),
      setSuspended: (request) => fixture.setSuspended?.(request) ?? {},
    })
    service(DiagnosticsService, { getDiagnostics: () => fixture.diagnostics ?? {} })
    service(WatchService, {
      subscribe: (_request, context) => (fixture.watch ?? quietWatch)(context.signal),
    })
  })
}

/** Renders the whole console at a path, against the fixture. */
export function renderConsole(path: string, fixture: Fixture = {}) {
  const history = createMemoryHistory({ initialEntries: [path] })
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <AppProviders transport={consoleTransport(fixture)} queryClient={queryClient}>
      <App history={history} />
    </AppProviders>
  )
  return { ...view, history, queryClient }
}

type WatchEvent = MessageInitShape<typeof SubscribeResponseSchema>

/** A watch the test drives: each push is the next event; an Error ends it. */
export function scriptedWatch() {
  const queue: (WatchEvent | Error)[] = []
  let wake: () => void = () => {}
  return {
    push(item: WatchEvent | Error) {
      queue.push(item)
      wake()
    },
    async *stream(signal: AbortSignal) {
      while (!signal.aborted) {
        const next = queue.shift()
        if (next instanceof Error) {
          throw next
        }
        if (next) {
          yield next
          continue
        }
        await new Promise<void>((resolve) => {
          wake = resolve
          signal.addEventListener("abort", () => resolve())
        })
      }
    },
  }
}
