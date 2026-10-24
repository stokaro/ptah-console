import { create } from "@bufbuild/protobuf"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  DecisionOutcome,
  ResourceKind,
  type RequestReadRequest,
} from "../../gen/ptah/console/v1/decisions_pb"
import { Answer, ReadRequestSchema } from "../../gen/ptah/console/v1/resources_pb"
import { renderConsole, syncedMigration, waitingSchema } from "../../test/console"

const asked = new Date(Date.now() - 2 * 60_000).toISOString()

describe("Read again now", () => {
  it("asks exactly the schema on the page to read its database", async () => {
    const user = userEvent.setup()
    const sent: RequestReadRequest[] = []
    const { container } = renderConsole("/console/schemas/application/storefront", {
      schemas: [waitingSchema()],
      migrations: [],
      requestRead: (request) => {
        sent.push(request)
        return { outcome: DecisionOutcome.ADMITTED, requested: new Date().toISOString() }
      },
    })

    await user.click(await screen.findByRole("button", { name: "Actions" }))
    await user.click(await screen.findByRole("menuitem", { name: "Read again now" }))

    await waitFor(() => expect(sent).toHaveLength(1))
    expect(sent[0]).toMatchObject({
      kind: ResourceKind.SCHEMA,
      namespace: "application",
      name: "storefront",
      uid: "uid-schema",
    })
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it.each([
    [
      "not yet answered",
      "",
      false,
      "A read was asked 2 min ago. The operator starts it once no operation is running.",
    ],
    ["answered", asked, false, "A read was asked 2 min ago, and the operator started it."],
    [
      "asked of a suspended migration",
      "",
      true,
      "The resource is suspended, so the operator answers once it is resumed.",
    ],
  ])("says where a request stands when %s", async (_, answered, suspended, says) => {
    renderConsole("/console/migrations/application/billing", {
      schemas: [],
      migrations: [
        syncedMigration((m) => {
          m.suspended = suspended
          m.readRequest = create(ReadRequestSchema, { requested: asked, answered })
        }),
      ],
    })

    expect(
      await screen.findByText(new RegExp(says.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")))
    ).toBeVisible()
  })

  it("shows the API server's refusal in its own words", async () => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront", {
      schemas: [waitingSchema()],
      migrations: [],
      requestRead: () => ({
        outcome: DecisionOutcome.CHANGED,
        message: "Another object holds this name now. Nothing was asked; look again.",
      }),
    })

    await user.click(await screen.findByRole("button", { name: "Actions" }))
    await user.click(await screen.findByRole("menuitem", { name: "Read again now" }))

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Another object holds this name now."
    )
  })

  it("is not offered to an identity that may not patch the resource", async () => {
    const user = userEvent.setup()
    renderConsole("/console/schemas/application/storefront", {
      schemas: [waitingSchema()],
      migrations: [],
      capabilities: { listSchemas: Answer.ALLOWED, listMigrations: Answer.ALLOWED },
    })

    await user.click(await screen.findByRole("button", { name: "Actions" }))

    const menu = await screen.findByRole("menu")
    expect(within(menu).getByRole("menuitem", { name: "Copy link" })).toBeVisible()
    expect(within(menu).queryByRole("menuitem", { name: "Read again now" })).not.toBeInTheDocument()
    expect(within(menu).queryByRole("menuitem", { name: "Suspend…" })).not.toBeInTheDocument()
  })
})
