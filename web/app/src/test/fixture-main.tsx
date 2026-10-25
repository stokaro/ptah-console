// The console against the test fixtures, for looking at the screens without
// a cluster: `npm run dev --workspace app`, then open /fixture.html. The
// production build reads index.html only, so none of this ships.
import { create } from "@bufbuild/protobuf"
import { createBrowserHistory } from "@tanstack/react-router"
import { QueryClient } from "@tanstack/react-query"
import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import { App } from "../App"
import { AppProviders } from "../AppProviders"
import { DecisionKind, DecisionOutcome, DecisionSchema } from "../gen/ptah/console/v1/decisions_pb"
import { applyAccent, readAccent } from "../accent"
import { applyTheme, readTheme } from "../theme"
import { Evidence } from "../gen/ptah/console/v1/diagnostics_pb"
import {
  JournalSchema,
  MigrationHistorySchema,
  MigrationPlanSchema,
  MigrationRevisionSchema,
  MigrationRunSchema,
  StageState,
  TargetSchema,
} from "../gen/ptah/console/v1/resources_pb"
import { consoleTransport, minutesAgo, result, syncedMigration, waitingSchema } from "./console"
import "../index.css"

applyTheme(readTheme())
applyAccent(readAccent())
// The router knows the console's paths, not this file's.
window.history.replaceState(null, "", "/approve")

const transport = consoleTransport({
  username: "system:serviceaccount:application:demo-approver",
  schemas: [
    waitingSchema((schema) => {
      schema.gitops = {
        $typeName: "ptah.console.v1.GitOps",
        controller: "Argo CD",
        application: "storefront",
      }
      schema.target = create(TargetSchema, {
        engine: "PostgreSQL",
        secretName: "storefront-database",
        secretKey: "url",
        lastObservedAt: minutesAgo(3),
        highestDriftSeverity: "destructive",
        driftFindingCount: 4,
        driftFindings: [
          { category: "columns_removed", count: 1, severity: "destructive" },
          { category: "columns_modified", count: 1, severity: "warning" },
          { category: "indexes_added", count: 1, severity: "safe" },
          { category: "rls_policies_added", count: 1, severity: "safe" },
        ],
        inventory: [
          { kind: "schemas", desired: 1, observed: 1 },
          { kind: "tables", desired: 14, observed: 14 },
          { kind: "columns", desired: 96, observed: 97 },
          { kind: "indexes", desired: 22, observed: 21 },
          { kind: "constraints", desired: 31, observed: 31 },
          { kind: "rls_policies", desired: 4, observed: 3 },
        ],
      })
      schema.journal = create(JournalSchema, {
        coveredSince: minutesAgo(900),
        entries: [
          { type: "Verified", at: minutesAgo(400), outcome: "InSync", plan: "storefront-p-71aa" },
          {
            type: "PlanPublished",
            at: minutesAgo(12),
            plan: "storefront-p-8f2c",
            statements: 3,
            destructive: true,
          },
        ],
      })
    }),
  ],
  // A sequence of 70 migrations, the run that stopped in 71, and 72 waiting.
  migrations: [
    syncedMigration((migration) => {
      migration.phase = "Failed"
      migration.planName = "billing-p-9"
      migration.stages[4].state = StageState.FAILED
      migration.history = create(MigrationHistorySchema, {
        observedAt: minutesAgo(1),
        currentVersion: 70n,
        appliedCount: 70,
        pendingCount: 2,
        revisions: Array.from({ length: 64 }, (_, index) => {
          const version = BigInt(70 - index)
          return create(MigrationRevisionSchema, {
            version,
            description: `step ${version}`,
            checksum: `h1:${version}a6f0c2b9d4e81f37${version}`,
            state: "applied",
            appliedAt: minutesAgo((71 - Number(version)) * 7),
            executionMillis: BigInt(((Number(version) * 37) % 2400) + 40),
          })
        }),
      })
      migration.lastRun = create(MigrationRunSchema, {
        outcome: "Failed",
        jobName: "ptah-m-apply-billing-9",
        startedAt: minutesAgo(3),
        finishedAt: minutesAgo(2),
        appliedVersions: [],
        message: "Migration 71 (read ledger) failed at statement 2 of 3 and committed nothing",
        failure: { version: 71n, statement: 2, statements: 3, sqlState: "42P01" },
      })
    }),
  ],
  migrationPlan: create(MigrationPlanSchema, {
    namespace: "application",
    migration: "billing",
    name: "billing-p-9",
    migrations: [
      { version: 71n, versionKey: "71", description: "read ledger", checksum: "h1:71" },
      { version: 72n, versionKey: "72", description: "add totals", checksum: "h1:72" },
    ],
  }),
  diagnostics: {
    events: {
      evidence: Evidence.SHOWN,
      events: [
        {
          type: "Warning",
          reason: "RunFailed",
          message: "Migration 71 failed",
          count: 2,
          lastSeen: minutesAgo(2),
        },
        {
          type: "Normal",
          reason: "PlanPublished",
          message: "Plan billing-p-9",
          count: 1,
          lastSeen: minutesAgo(9),
        },
      ],
    },
  },
  approve: async (request) => {
    await new Promise((resolve) => setTimeout(resolve, 600))
    return result({
      outcome: DecisionOutcome.ADMITTED,
      decision: create(DecisionSchema, {
        kind: DecisionKind.SCHEMA_APPROVAL,
        planName: request.planName,
        decidedBy: "system:serviceaccount:application:demo-approver",
      }),
    })
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AppProviders transport={transport} queryClient={new QueryClient()}>
      <App history={createBrowserHistory()} />
    </AppProviders>
  </StrictMode>
)
