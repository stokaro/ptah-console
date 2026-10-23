import { create } from "@bufbuild/protobuf"
import { screen, within } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { PlanSourceSchema, SourceKind, SourceSchema } from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, storedPlan, waitingSchema } from "../../test/console"

const commit = "3f1c2b0a9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a"
const content = "sha256:1111aaaa00000000000000000000000000000000000000000000000022220000"
const policyDigest = "sha256:5050bbbb00000000000000000000000000000000000000000000000033330000"

const gitSchema = waitingSchema((s) => {
  s.source = create(SourceSchema, {
    kind: SourceKind.GIT,
    requested: "https://git.example.com/acme/app.git",
    ref: "branch main",
    path: "db/schema.hcl",
    resolved: commit,
    contentDigest: content,
    policy: { name: "team-sources", uid: "uid-policy", digest: policyDigest },
    resolvedAt: minutesAgo(4),
  })
})

const configMapSchema = waitingSchema((s) => {
  s.source = create(SourceSchema, {
    kind: SourceKind.CONFIG_MAP,
    requested: "app-schema",
    resolved: "ptah-source-0a1b2c3d4e5f6a7b8c9d0e1f",
    contentDigest: content,
  })
})

describe("A source with no verification policy", () => {
  it("shows the repository, ref, path and commit of a Git source and the policy it was read under", async () => {
    const { container } = renderConsole("/console/schemas/application/storefront", {
      schemas: [gitSchema],
    })

    const source = await screen.findByRole("region", { name: "Source" })
    expect(source).toHaveTextContent("https://git.example.com/acme/app.git")
    expect(source).toHaveTextContent("branch main")
    expect(source).toHaveTextContent("db/schema.hcl")
    expect(source).toHaveTextContent(commit)
    expect(source).toHaveTextContent(content)
    expect(source).toHaveTextContent("team-sources")
    expect(within(source).getByTitle(policyDigest)).toBeInTheDocument()
    // A Git source has no signature to verify, and saying it was not
    // verified would read as a gate it failed.
    expect(source).not.toHaveTextContent("Not verified")
    expect(screen.queryByRole("region", { name: "Artifact" })).toBeNull()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("shows the ConfigMap and the snapshot it was copied into, and no policy", async () => {
    renderConsole("/console/schemas/application/storefront", { schemas: [configMapSchema] })

    const source = await screen.findByRole("region", { name: "Source" })
    expect(source).toHaveTextContent("app-schema")
    expect(source).toHaveTextContent("ptah-source-0a1b2c3d4e5f6a7b8c9d0e1f")
    expect(source).toHaveTextContent("Source policyNone")
    expect(source).not.toHaveTextContent("Repository")
  })

  it("binds a plan to the source's content and policy in place of an artifact", async () => {
    renderConsole("/approve/application/schema/storefront", {
      schemas: [gitSchema],
      plan: storedPlan((plan) => {
        plan.contractVersion = 2
        plan.artifactDigest = ""
        plan.source = create(PlanSourceSchema, {
          kind: SourceKind.GIT,
          revision: "sha256:7070",
          contentDigest: content,
          policyUid: "uid-policy",
          policyDigest,
        })
      }),
    })

    const bound = await screen.findByRole("region", { name: "Bound to" })
    expect(bound).toHaveTextContent("Git, content")
    expect(bound).toHaveTextContent("Source policy")
    expect(bound).toHaveTextContent("Plan contract2")
    expect(bound).not.toHaveTextContent("Artifact")
  })
})
