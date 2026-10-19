import { create } from "@bufbuild/protobuf"
import { screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { AttentionKind, MigrationSchema } from "../gen/ptah/console/v1/resources_pb"
import { renderConsole, waitingSchema } from "../test/console"

const unresolved = create(MigrationSchema, {
  ref: { namespace: "application", name: "billing", uid: "uid-migration" },
  attention: [{ kind: AttentionKind.UNRESOLVED_RUN, reason: "Run op-2 is unresolved." }],
  unresolvedRun: { operationId: "op-2" },
})

// A read-only console is told so on every page that would offer a decision,
// whatever RBAC allows the identity.
describe("A read-only console", () => {
  it("offers no approval in the queue", async () => {
    renderConsole("/approve", { readOnly: true, schemas: [waitingSchema()] })

    expect(await screen.findByRole("link", { name: "Review" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Approve…" })).toBeNull()
    expect(screen.getByText("nothing: this console runs read-only")).toBeInTheDocument()
  })

  it("offers no approval on the plan", async () => {
    renderConsole("/approve/application/schema/storefront", {
      readOnly: true,
      schemas: [waitingSchema()],
    })

    expect(
      await screen.findByText("This console runs read-only: it sends no decision.")
    ).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Approve this plan…" })).toBeNull()
  })

  it("offers no acknowledgment and no suspend", async () => {
    renderConsole("/incidents/application/migration/billing", {
      readOnly: true,
      migrations: [unresolved],
    })
    expect(
      await screen.findByText("This console runs read-only: it sends no decision.")
    ).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Acknowledge this run…" })).toBeNull()
  })

  it("offers no suspend on a release", async () => {
    renderConsole("/releases/application/schema/storefront", {
      readOnly: true,
      schemas: [waitingSchema()],
    })

    expect(
      await screen.findByRole("button", { name: "Copy link for approvers" })
    ).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Suspend…" })).toBeNull()
    expect(screen.getByText("Read-only")).toBeInTheDocument()
  })
})
