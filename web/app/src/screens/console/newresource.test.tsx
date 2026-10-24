import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import type { ResourceDraft } from "../../gen/ptah/console/v1/authoring_pb"
import { DecisionOutcome } from "../../gen/ptah/console/v1/decisions_pb"
import { Answer } from "../../gen/ptah/console/v1/resources_pb"
import { renderConsole } from "../../test/console"

type Sent = { kind: string; draft?: ResourceDraft; dryRun: boolean }

/** A console that records what the page sends and answers as the API server would. */
function recording(answer?: (sent: Sent) => { outcome: DecisionOutcome; message?: string }) {
  const sent: Sent[] = []
  return {
    sent,
    authoring: (
      kind: "schema" | "migration",
      request: { draft?: ResourceDraft; dryRun: boolean }
    ) => {
      sent.push({ kind, draft: request.draft, dryRun: request.dryRun })
      const verdict = answer?.({ kind, draft: request.draft, dryRun: request.dryRun }) ?? {
        outcome: DecisionOutcome.ADMITTED,
      }
      return {
        ...verdict,
        dryRun: request.dryRun,
        yaml:
          verdict.outcome === DecisionOutcome.ADMITTED
            ? `kind: PtahSchema\nspec:\n  policy:\n    apply: OnApproval\n`
            : "",
        ref: { namespace: request.draft?.namespace, name: request.draft?.name },
      }
    },
  }
}

async function fillOCI(user: ReturnType<typeof userEvent.setup>) {
  const form = await screen.findByRole("form", { name: "New PtahSchema" })
  await user.type(within(form).getByLabelText("Name"), "storefront")
  await user.type(within(form).getByLabelText("Coordination key"), "production/storefront")
  await user.type(within(form).getByLabelText("Connection Secret"), "storefront-database")
  await user.type(within(form).getByLabelText("Artifact"), "oci://ghcr.io/example/storefront:1.0.0")
  await user.type(within(form).getByLabelText("Verification policy"), "ptah-verification-policy")
  return form
}

describe("A new schema or migration", () => {
  it("checks the draft with the API server first, and creates only what was checked", async () => {
    const user = userEvent.setup()
    const lab = recording()
    const { container } = renderConsole("/console/new", {
      schemas: [],
      migrations: [],
      authoring: lab.authoring,
    })

    expect(await screen.findByRole("heading", { level: 1, name: "New PtahSchema" })).toBeVisible()
    const check = screen.getByRole("button", { name: "Check with the API server" })
    const create = screen.getByRole("button", { name: "Create PtahSchema" })
    expect(check).toBeDisabled()
    const form = screen.getByRole("form", { name: "New PtahSchema" })
    expect(within(form).getByRole("status")).toHaveTextContent(
      "Still empty: Name, Coordination key, Connection Secret, Artifact, Verification policy."
    )
    await fillOCI(user)
    expect(create).toBeDisabled()

    await user.click(check)

    expect(await screen.findByText("The API server accepts this PtahSchema")).toBeVisible()
    expect(screen.getByLabelText("What would be created")).toHaveTextContent("apply: OnApproval")
    expect(lab.sent).toHaveLength(1)
    expect(lab.sent[0]).toMatchObject({ kind: "schema", dryRun: true })
    expect(lab.sent[0].draft).toMatchObject({
      namespace: "application",
      name: "storefront",
      applyPolicy: "OnApproval",
      target: {
        engine: "PostgreSQL",
        coordinationKey: "production/storefront",
        realm: "",
        urlSecretName: "storefront-database",
        urlSecretKey: "url",
      },
      source: {
        kind: {
          case: "oci",
          value: {
            ref: "oci://ghcr.io/example/storefront:1.0.0",
            verificationPolicyName: "ptah-verification-policy",
            verificationPolicyKey: "policy.yaml",
          },
        },
      },
    })
    expect(create).toBeEnabled()
    expect(await axe(container)).toHaveNoViolations()

    await user.click(create)

    expect(await screen.findByText("Created PtahSchema storefront")).toBeVisible()
    expect(lab.sent[1]).toMatchObject({ kind: "schema", dryRun: false })
    expect(screen.getByRole("link", { name: "Open application/storefront" })).toHaveAttribute(
      "href",
      "/console/schemas/application/storefront"
    )
  })

  it("asks for another check once the draft changes after one", async () => {
    const user = userEvent.setup()
    const lab = recording()
    renderConsole("/console/new", { schemas: [], migrations: [], authoring: lab.authoring })
    const form = await fillOCI(user)
    await user.click(screen.getByRole("button", { name: "Check with the API server" }))
    await screen.findByText("The API server accepts this PtahSchema")

    await user.type(within(form).getByLabelText("Name"), "-2")

    expect(screen.getByRole("button", { name: "Create PtahSchema" })).toBeDisabled()
    expect(within(form).getByRole("status")).toHaveTextContent(
      "The draft changed since the check; check it again."
    )
  })

  it("shows the API server's refusal in its own words, and offers no create", async () => {
    const user = userEvent.setup()
    const lab = recording(() => ({
      outcome: DecisionOutcome.REFUSED,
      message:
        'PtahSchema.operator.ptah.run "storefront" is invalid: spec.target: set exactly one of coordinationKey and realmRef',
    }))
    renderConsole("/console/new", { schemas: [], migrations: [], authoring: lab.authoring })
    await fillOCI(user)

    await user.click(screen.getByRole("button", { name: "Check with the API server" }))

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "set exactly one of coordinationKey and realmRef"
    )
    expect(screen.getByText("The API server refuses this PtahSchema")).toBeVisible()
    expect(screen.getByRole("button", { name: "Create PtahSchema" })).toBeDisabled()
  })

  it("sends a migration from a Git branch with the fields that template uses", async () => {
    const user = userEvent.setup()
    const lab = recording()
    renderConsole("/console/new?kind=migration&template=git", {
      schemas: [],
      migrations: [],
      authoring: lab.authoring,
    })
    const form = await screen.findByRole("form", { name: "New PtahMigration" })
    await user.type(within(form).getByLabelText("Name"), "billing")
    await user.click(within(form).getByRole("button", { name: "A realm an administrator granted" }))
    await user.type(within(form).getByLabelText("Realm"), "billing-primary")
    await user.type(within(form).getByLabelText("Connection Secret"), "billing-database")
    await user.type(
      within(form).getByLabelText("Repository"),
      "https://git.example.com/shop/migrations.git"
    )
    await user.type(within(form).getByLabelText("Branch"), "main")
    await user.type(within(form).getByLabelText("Path"), "migrations")

    await user.click(screen.getByRole("button", { name: "Check with the API server" }))

    await waitFor(() => expect(lab.sent).toHaveLength(1))
    expect(lab.sent[0]).toMatchObject({ kind: "migration", dryRun: true })
    expect(lab.sent[0].draft).toMatchObject({
      name: "billing",
      target: { coordinationKey: "", realm: "billing-primary" },
      source: {
        kind: {
          case: "git",
          value: {
            url: "https://git.example.com/shop/migrations.git",
            branch: "main",
            tag: "",
            commit: "",
            path: "migrations",
            credentialsSecret: "",
          },
        },
      },
    })
  })

  it("offers nowhere to create one when RBAC lets this identity create none", async () => {
    renderConsole("/console/new", {
      schemas: [],
      migrations: [],
      capabilities: { listSchemas: Answer.ALLOWED, listMigrations: Answer.ALLOWED },
    })

    expect(await screen.findByText("Nowhere to create one")).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "Check with the API server" })
    ).not.toBeInTheDocument()
  })

  it("sends nothing from a read-only console, and says why", async () => {
    const user = userEvent.setup()
    const lab = recording()
    renderConsole("/console/new", {
      schemas: [],
      migrations: [],
      readOnly: true,
      authoring: lab.authoring,
    })
    const form = await fillOCI(user)

    expect(screen.getByRole("button", { name: "Check with the API server" })).toBeDisabled()
    expect(within(form).getByRole("status")).toHaveTextContent("This console runs read-only")
    expect(lab.sent).toHaveLength(0)
  })

  it("is offered from the resource list and the palette", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console/resources", { schemas: [], migrations: [] })

    await user.click(await screen.findByRole("link", { name: "New migration" }))

    await waitFor(() => expect(history.location.pathname).toBe("/console/new"))
    expect(history.location.search).toContain("kind=migration")
    expect(
      await screen.findByRole("heading", { level: 1, name: "New PtahMigration" })
    ).toBeVisible()
  })
})
