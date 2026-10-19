import { create, type MessageInitShape } from "@bufbuild/protobuf"
import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { createRouterTransport } from "@connectrpc/connect"
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
import { InfoService, Mode } from "../gen/ptah/console/v1/info_pb"
import {
  Access,
  Answer,
  AttentionKind,
  MigrationService,
  SchemaPlanSchema,
  SchemaSchema,
  SchemaService,
  ScopeService,
  SqlAccess,
  StageState,
  type CapabilitiesSchema,
  type OperatorApiSchema,
  type Migration,
  type Schema,
  type SchemaPlan,
} from "../gen/ptah/console/v1/resources_pb"
import { SessionService } from "../gen/ptah/console/v1/session_pb"
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
  migrations?: Migration[]
  decisions?: Decision[]
  plan?: SchemaPlan
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
  /** The live stream; one heartbeat, then quiet, by default. */
  watch?: (signal: AbortSignal) => AsyncIterable<MessageInitShape<typeof SubscribeResponseSchema>>
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
  return createRouterTransport(({ service }) => {
    service(InfoService, { getInfo: () => ({ version: "v0.1.0", mode: Mode.LOCAL }) })
    service(SessionService, {
      getSession: () => ({
        mode: Mode.LOCAL,
        context: "kind-ptah",
        kubernetesUsername: fixture.username ?? "alice",
        readOnly: fixture.readOnly ?? false,
      }),
    })
    service(ScopeService, {
      listNamespaces: () => ({
        namespaces: [
          { name: "application", access: Access.OK, capabilities: fixture.capabilities ?? allowed },
        ],
        operatorApi: fixture.operatorApi ?? { served: true },
      }),
    })
    service(SchemaService, {
      listSchemas: () => ({
        schemas: fixture.schemas ?? [],
        namespaces: [
          {
            namespace: "application",
            access: Access.OK,
            truncated: (fixture.truncated ?? []).includes("application"),
          },
        ],
      }),
      getSchemaPlan: () => ({ plan: fixture.plan ?? storedPlan() }),
    })
    service(MigrationService, {
      listMigrations: () => ({ migrations: fixture.migrations ?? [] }),
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
  return { ...view, history }
}
