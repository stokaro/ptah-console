import { create } from "@bufbuild/protobuf"
import { createRouterTransport } from "@connectrpc/connect"
import type { Meta, StoryObj } from "@storybook/react-vite"
import { QueryClient } from "@tanstack/react-query"
import { useState } from "react"

import { AppProviders } from "../../AppProviders"
import { DecisionOutcome, DecisionService } from "../../gen/ptah/console/v1/decisions_pb"
import { Mode } from "../../gen/ptah/console/v1/info_pb"
import {
  MigrationSchema,
  OperationSchema,
  SchemaSchema,
  SourceKind,
} from "../../gen/ptah/console/v1/resources_pb"
import { SessionService } from "../../gen/ptah/console/v1/session_pb"
import { fromMigration, fromSchema, type ResourceView } from "../../model/resource"
import { ChangeSourceDialog } from "./ChangeSourceDialog"

const current = `oci://registry.example/schemas/storefront@sha256:${"5f2c".repeat(16)}`

const storefront = fromSchema(
  create(SchemaSchema, {
    ref: { namespace: "application", name: "storefront", uid: "uid-schema", generation: 4n },
    applyPolicy: "OnApproval",
    allowDestructive: false,
    source: { kind: SourceKind.OCI, requested: current, specReference: current },
  })
)

/**
 * The API in memory: the session, and a change of source that the API
 * server accepts as a dry run and stores at generation 5.
 */
function transport() {
  return createRouterTransport(({ service }) => {
    service(SessionService, {
      getSession: () => ({ mode: Mode.LOCAL, context: "kind-ptah", kubernetesUsername: "alice" }),
    })
    service(DecisionService, {
      setSource: (request) => ({
        outcome: DecisionOutcome.ADMITTED,
        dryRun: request.dryRun,
        changed: !request.dryRun,
        field: "spec.desired.ociRef",
        generation: 5n,
        sourceYaml: `desired:\n  ociRef: ${request.reference}\n  verificationPolicyFrom:\n    key: policy.yaml\n    name: ptah-verification-policy\n`,
      }),
    })
  })
}

/** The dialog over the in-memory API, as the resource page opens it. */
function Dialog({ resource }: { resource: ResourceView }) {
  const [api] = useState(transport)
  const [queries] = useState(() => new QueryClient())
  return (
    <AppProviders transport={api} queryClient={queries}>
      <ChangeSourceDialog resource={resource} context="kind-ptah" onClose={() => {}} />
    </AppProviders>
  )
}

const meta = {
  title: "Console/ChangeSourceDialog",
  component: Dialog,
  args: { resource: storefront },
} satisfies Meta<typeof Dialog>

export default meta
type Story = StoryObj<typeof meta>

/** A schema on a pinned artifact, under OnApproval, destructive changes refused. */
export const Schema: Story = {}

/** A suspended migration that applies by itself: nothing resolves until it is resumed. */
export const SuspendedMigration: Story = {
  args: {
    resource: fromMigration(
      create(MigrationSchema, {
        ref: { namespace: "application", name: "billing", uid: "uid-migration" },
        applyPolicy: "Always",
        suspended: true,
        source: { kind: SourceKind.OCI, requested: current, specReference: current },
      })
    ),
  },
}

/** An Apply whose Job may exist holds the change back. */
export const ApplyRunning: Story = {
  args: {
    resource: fromSchema(
      create(SchemaSchema, {
        ref: { namespace: "application", name: "storefront", uid: "uid-schema" },
        applyPolicy: "OnApproval",
        allowDestructive: true,
        source: { kind: SourceKind.OCI, requested: current, specReference: current },
        activeOperation: create(OperationSchema, {
          type: "Apply",
          jobName: "storefront-apply-8f2c",
          dispatched: true,
        }),
      })
    ),
  },
}
