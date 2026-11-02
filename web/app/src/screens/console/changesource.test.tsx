import { create } from "@bufbuild/protobuf"
import { Code, ConnectError } from "@connectrpc/connect"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  DecisionOutcome,
  ResourceKind,
  type SetSourceRequest,
} from "../../gen/ptah/console/v1/decisions_pb"
import {
  Answer,
  OperationSchema,
  SourceKind,
  type Migration,
  type Schema,
} from "../../gen/ptah/console/v1/resources_pb"
import { WatchedKind } from "../../gen/ptah/console/v1/watch_pb"
import {
  heartbeat,
  renderConsole,
  scriptedWatch,
  syncedMigration,
  waitingSchema,
  type Fixture,
} from "../../test/console"

const current = `oci://registry.example/schemas/demo@sha256:${"a".repeat(64)}`
const next = `oci://registry.example/schemas/demo@sha256:${"b".repeat(64)}`

/** storefront, at rest on an OCI artifact pinned by digest. */
function ociSchema(change: (schema: Schema) => void = () => {}) {
  return waitingSchema((schema) => {
    schema.source!.kind = SourceKind.OCI
    schema.source!.requested = current
    schema.source!.specReference = current
    change(schema)
  })
}

function ociMigration(change: (migration: Migration) => void = () => {}) {
  return syncedMigration((migration) => {
    migration.source!.kind = SourceKind.OCI
    migration.source!.requested = current
    migration.source!.specReference = current
    change(migration)
  })
}

const changed = () => ({
  event: {
    case: "invalidate" as const,
    value: { kind: WatchedKind.SCHEMA, namespace: "application", name: "storefront" },
  },
})

async function openDialog(fixture: Fixture, path = "/console/schemas/application/storefront") {
  const user = userEvent.setup()
  const view = renderConsole(path, { migrations: [], schemas: [], ...fixture })
  await user.click(await screen.findByRole("button", { name: "Actions" }))
  await user.click(await screen.findByRole("menuitem", { name: "Change source…" }))
  const dialog = await screen.findByRole("alertdialog")
  return { user, dialog, ...view }
}

describe("Change source", () => {
  it("checks the reference with the API server first and sends only what that check accepted", async () => {
    const sent: SetSourceRequest[] = []
    const { user, dialog } = await openDialog({
      schemas: [ociSchema()],
      setSource: (request) => {
        sent.push(request)
        return {
          outcome: DecisionOutcome.ADMITTED,
          dryRun: request.dryRun,
          changed: !request.dryRun,
          field: "spec.desired.ociRef",
          generation: 5n,
          sourceYaml: `desired:\n  ociRef: ${request.reference}\n  verificationPolicyFrom:\n    key: policy.yaml\n    name: ptah-verification-policy\n`,
        }
      },
    })
    expect(within(dialog).getByText(current)).toBeVisible()
    const send = within(dialog).getByRole("button", { name: "Change source" })
    const check = within(dialog).getByRole("button", { name: "Check with the API server" })
    expect(check).toBeDisabled()

    await user.type(within(dialog).getByLabelText("New reference"), next)
    expect(send).toBeDisabled()
    await user.click(check)

    expect(await within(dialog).findByText("The API server accepts this change")).toBeVisible()
    expect(within(dialog).getByLabelText("The source as it would be stored")).toHaveTextContent(
      "name: ptah-verification-policy"
    )
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({
      kind: ResourceKind.SCHEMA,
      namespace: "application",
      name: "storefront",
      uid: "uid-schema",
      fromReference: current,
      reference: next,
      dryRun: true,
    })

    await user.click(within(dialog).getByRole("button", { name: "Change source" }))

    expect(await within(dialog).findByText("Source changed")).toBeVisible()
    expect(dialog).toHaveTextContent(
      "spec.desired.ociRef of storefront names the new artifact, at generation 5."
    )
    expect(sent).toHaveLength(2)
    expect(sent[1]).toMatchObject({ fromReference: current, reference: next, dryRun: false })
  })

  it("asks for the check again once the reference changes", async () => {
    const { user, dialog } = await openDialog({ schemas: [ociSchema()] })
    const input = within(dialog).getByLabelText("New reference")
    await user.type(input, next)
    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))
    await within(dialog).findByText("The API server accepts this change")

    await user.clear(input)
    await user.type(input, next.replace(/b{64}$/, "c".repeat(64)))

    expect(within(dialog).queryByText("The API server accepts this change")).not.toBeInTheDocument()
    expect(within(dialog).getByRole("button", { name: "Change source" })).toBeDisabled()
  })

  it.each([
    [
      "a tag",
      "oci://registry.example/schemas/demo:1.5.0",
      '"oci://registry.example/schemas/demo:1.5.0" names tag 1.5.0, not a digest',
    ],
    [
      "a tag beside the digest",
      `oci://registry.example/schemas/demo:1.5.0@sha256:${"b".repeat(64)}`,
      "names tag 1.5.0 beside its digest",
    ],
    [
      "a nested scheme",
      `oci://registry.example/oci://other.example/demo@sha256:${"b".repeat(64)}`,
      "a repository path carries no scheme",
    ],
    ["the reference it names now", current, "This is the reference spec.desired.ociRef names now."],
  ])("refuses %s inline, before anything is sent", async (_, value, says) => {
    const sent: SetSourceRequest[] = []
    const { user, dialog } = await openDialog({
      schemas: [ociSchema()],
      setSource: (request) => {
        sent.push(request)
        return {}
      },
    })
    const input = within(dialog).getByLabelText("New reference")

    await user.type(input, value)

    expect(input).toHaveAttribute("aria-invalid", "true")
    expect(within(dialog).getByText(says, { exact: false })).toBeVisible()
    expect(within(dialog).getByRole("button", { name: "Check with the API server" })).toBeDisabled()
    expect(sent).toHaveLength(0)
  })

  it.each([
    [
      "changed",
      DecisionOutcome.CHANGED,
      "It changed",
      "spec.desired.ociRef changed since you looked",
    ],
    [
      "forbidden",
      DecisionOutcome.FORBIDDEN,
      "Kubernetes does not let you",
      'User "alice" cannot patch resource "ptahschemas"',
    ],
    [
      "refused",
      DecisionOutcome.REFUSED,
      "Refused",
      'admission webhook "mschemawriter.operator.ptah.run" denied the request',
    ],
    [
      "unknown",
      DecisionOutcome.UNKNOWN,
      "Submission outcome unknown",
      "The answer was lost. The change may have been stored",
    ],
  ])("reports a change that was %s as such", async (_, outcome, title, message) => {
    const { user, dialog } = await openDialog({
      schemas: [ociSchema()],
      setSource: (request) =>
        request.dryRun
          ? { outcome: DecisionOutcome.ADMITTED, dryRun: true }
          : { outcome, message, field: "spec.desired.ociRef" },
    })
    await user.type(within(dialog).getByLabelText("New reference"), next)
    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))
    await within(dialog).findByText("The API server accepts this change")

    await user.click(within(dialog).getByRole("button", { name: "Change source" }))

    expect(await within(dialog).findByText(title)).toBeVisible()
    expect(dialog).toHaveTextContent(message)
  })

  it("reads the resource back after an answer that was lost", async () => {
    let stored = current
    const { user, dialog } = await openDialog({
      schemas: () => [ociSchema((schema) => (schema.source!.specReference = stored))],
      setSource: (request) => {
        if (request.dryRun) {
          return { outcome: DecisionOutcome.ADMITTED, dryRun: true }
        }
        stored = request.reference
        return { outcome: DecisionOutcome.UNKNOWN, message: "The answer was lost." }
      },
    })
    await user.type(within(dialog).getByLabelText("New reference"), next)
    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))
    await within(dialog).findByText("The API server accepts this change")
    await user.click(within(dialog).getByRole("button", { name: "Change source" }))

    await user.click(await within(dialog).findByRole("button", { name: "Check result" }))

    expect(await within(dialog).findByText("Source changed")).toBeVisible()
    expect(dialog).toHaveTextContent("The change was stored")
  })

  it("shows the API server's refusal of the check in its own words", async () => {
    const { user, dialog } = await openDialog({
      schemas: [ociSchema()],
      setSource: () => ({
        outcome: DecisionOutcome.REFUSED,
        dryRun: true,
        message: 'PtahSchema.operator.ptah.run "storefront" is invalid: spec.desired.ociRef',
      }),
    })
    await user.type(within(dialog).getByLabelText("New reference"), next)

    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("spec.desired.ociRef")
    expect(within(dialog).getByText("The API server refuses this change")).toBeVisible()
    expect(within(dialog).getByRole("button", { name: "Change source" })).toBeDisabled()
  })

  it("says what happens next for a suspended migration that applies by itself", async () => {
    const { dialog } = await openDialog(
      { migrations: [ociMigration((m) => (m.suspended = true))] },
      "/console/migrations/application/billing"
    )

    const next = within(dialog).getByRole("region", { name: "What happens next" })
    expect(next).toHaveTextContent(
      "The resource is suspended: the operator starts no Job until it is resumed"
    )
    expect(next).toHaveTextContent(
      "reads the migration history and plans the versions still pending"
    )
    expect(next).toHaveTextContent(
      "Under spec.policy.apply Always that plan runs as soon as it is published, without a person."
    )
    expect(dialog).toHaveTextContent("spec.artifact.ociRef")
    expect(dialog).toHaveTextContent("ptah migrations push --format reference")
  })

  it("holds the change back while an Apply whose Job may exist runs", async () => {
    const { user, dialog } = await openDialog({
      schemas: [
        ociSchema((schema) => {
          schema.activeOperation = create(OperationSchema, {
            type: "Apply",
            jobName: "storefront-apply-8f2c",
            dispatched: true,
          })
        }),
      ],
    })

    await user.type(within(dialog).getByLabelText("New reference"), next)

    expect(within(dialog).getByRole("alert")).toHaveTextContent(
      "An Apply is running (Job storefront-apply-8f2c)"
    )
    expect(within(dialog).getByRole("button", { name: "Check with the API server" })).toBeDisabled()
  })

  it("passes axe with a check answered", async () => {
    const { user, dialog } = await openDialog({ schemas: [ociSchema()] })
    await user.type(within(dialog).getByLabelText("New reference"), next)
    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))
    await within(dialog).findByText("The API server accepts this change")

    expect(await axe(document.body)).toHaveNoViolations()
  })

  it.each([
    [
      "an identity that may not patch the resource",
      { schemas: [ociSchema()], capabilities: { listSchemas: Answer.ALLOWED } },
    ],
    [
      "a ConfigMap source",
      { schemas: [ociSchema((schema) => (schema.source!.kind = SourceKind.CONFIG_MAP))] },
    ],
    ["a Git source", { schemas: [ociSchema((schema) => (schema.source!.kind = SourceKind.GIT))] }],
    ["a read-only console", { schemas: [ociSchema()], readOnly: true }],
  ])("is not offered to %s", async (_, fixture: Fixture) => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront", { migrations: [], ...fixture })

    await user.click(await screen.findByRole("button", { name: "Actions" }))

    const menu = await screen.findByRole("menu")
    await waitFor(() =>
      expect(within(menu).getByRole("menuitem", { name: "Copy link" })).toBeVisible()
    )
    expect(within(menu).queryByRole("menuitem", { name: "Change source…" })).not.toBeInTheDocument()
  })

  it("sends the check and the change only to the object the dialog opened on", async () => {
    const watch = scriptedWatch()
    let uid = "uid-schema"
    const sent: SetSourceRequest[] = []
    const { user, dialog } = await openDialog({
      schemas: () => [ociSchema((schema) => (schema.ref!.uid = uid))],
      watch: watch.stream,
      setSource: (request) => {
        sent.push(request)
        return {
          outcome: DecisionOutcome.ADMITTED,
          dryRun: request.dryRun,
          sourceYaml: "desired: {}\n",
        }
      },
    })
    watch.push(heartbeat())
    await user.type(within(dialog).getByLabelText("New reference"), next)
    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))
    await within(dialog).findByText("The API server accepts this change")

    uid = "uid-recreated"
    watch.push(changed())

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "storefront was deleted and created again since this dialog opened"
    )
    expect(within(dialog).getByRole("button", { name: "Change source" })).toBeDisabled()
    expect(within(dialog).getByRole("button", { name: "Check with the API server" })).toBeDisabled()
    expect(sent.map((request) => request.uid)).toEqual(["uid-schema"])
  })

  it("lets the change go once an Apply that held it back ends", async () => {
    const watch = scriptedWatch()
    let applying = true
    const { user, dialog } = await openDialog({
      schemas: () => [
        ociSchema((schema) => {
          schema.activeOperation = applying
            ? create(OperationSchema, {
                type: "Apply",
                jobName: "storefront-apply-8f2c",
                dispatched: true,
              })
            : undefined
        }),
      ],
      watch: watch.stream,
    })
    watch.push(heartbeat())
    await user.type(within(dialog).getByLabelText("New reference"), next)
    expect(within(dialog).getByRole("alert")).toHaveTextContent("An Apply is running")

    applying = false
    watch.push(changed())

    await waitFor(() =>
      expect(
        within(dialog).getByRole("button", { name: "Check with the API server" })
      ).toBeEnabled()
    )
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument()
  })

  it("treats a change whose answer was lost as unknown, and reads it back", async () => {
    const { user, dialog } = await openDialog({
      schemas: [ociSchema()],
      setSource: (request) => {
        if (request.dryRun) {
          return { outcome: DecisionOutcome.ADMITTED, dryRun: true }
        }
        throw new ConnectError("the decision may or may not have been made", Code.Unknown)
      },
    })
    await user.type(within(dialog).getByLabelText("New reference"), next)
    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))
    await within(dialog).findByText("The API server accepts this change")

    await user.click(within(dialog).getByRole("button", { name: "Change source" }))

    expect(await within(dialog).findByText("Submission outcome unknown")).toBeVisible()
    await user.click(within(dialog).getByRole("button", { name: "Check result" }))
    expect(await within(dialog).findByText("Not changed")).toBeVisible()
  })

  it("says a check that got no answer stored nothing", async () => {
    const { user, dialog } = await openDialog({
      schemas: [ociSchema()],
      setSource: () => {
        throw new ConnectError("the API server did not answer", Code.Unavailable)
      },
    })
    await user.type(within(dialog).getByLabelText("New reference"), next)

    await user.click(within(dialog).getByRole("button", { name: "Check with the API server" }))

    expect(await within(dialog).findByText("The API server did not answer")).toBeVisible()
    expect(dialog).toHaveTextContent("Nothing was stored.")
    expect(within(dialog).getByRole("button", { name: "Change source" })).toBeDisabled()
  })

  it("says why the change cannot be sent before a check", async () => {
    const { dialog } = await openDialog({ schemas: [ociSchema()] })

    expect(
      within(dialog).getByRole("button", { name: "Change source" })
    ).toHaveAccessibleDescription(
      "Only a reference the API server accepted in a check can be sent."
    )
  })
})
