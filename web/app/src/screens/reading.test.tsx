import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { WatchedKind } from "../gen/ptah/console/v1/watch_pb"
import { heartbeat, renderConsole, scriptedWatch, waitingSchema } from "../test/console"

// Each interval the operator reads a schema waiting for approval again. Its
// plan stays current and approval stays required, and admission refuses an
// approval until the read ends, so the console offers none while it runs.
const beingRead = () =>
  waitingSchema((schema) => {
    schema.phase = "Resolving"
    schema.approvalOpen = false
  })

const invalidate = () => ({
  event: {
    case: "invalidate" as const,
    value: { kind: WatchedKind.SCHEMA, namespace: "application", name: "storefront" },
  },
})

const sentence = /The operator is reading storefront again, as it does each interval\./

describe("A plan the operator is reading again", () => {
  it("is offered for approval on its page once the read ends", async () => {
    const watch = scriptedWatch()
    let current = beingRead()
    const { container } = renderConsole("/approve/application/schema/storefront", {
      schemas: () => [current],
      watch: watch.stream,
    })
    watch.push(heartbeat())

    expect(await screen.findByText(sentence)).toBeVisible()
    expect(await screen.findByRole("list", { name: "SQL statements" })).toBeVisible()
    expect(screen.queryByRole("button", { name: "Approve this plan…" })).not.toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()

    current = waitingSchema()
    watch.push(invalidate())
    expect(
      await screen.findByRole("button", { name: "Approve this plan…" }, { timeout: 3000 })
    ).toBeEnabled()
    expect(screen.queryByText(sentence)).not.toBeInTheDocument()
  })

  it("is listed in the queue with Review and without Approve", async () => {
    renderConsole("/approve", { schemas: [beingRead()] })

    const queue = await screen.findByRole("list", { name: "Plans waiting for approval" })
    const item = within(queue)
    expect(item.getByText(sentence)).toBeVisible()
    expect(item.getByRole("link", { name: "Review" })).toBeVisible()
    expect(item.queryByRole("button", { name: "Approve…" })).not.toBeInTheDocument()
  })

  it("holds Confirm in an open dialog while a read runs, and gives it back after", async () => {
    const user = userEvent.setup()
    const watch = scriptedWatch()
    let current = waitingSchema()
    renderConsole("/approve", { schemas: () => [current], watch: watch.stream })
    watch.push(heartbeat())
    await user.click(await screen.findByRole("button", { name: "Approve…" }))
    const dialog = await screen.findByRole("alertdialog")
    await user.click(within(dialog).getByRole("checkbox"))
    const confirm = within(dialog).getByRole("button", { name: "Confirm" })
    await waitFor(() => expect(confirm).toBeEnabled())

    current = beingRead()
    watch.push(invalidate())
    await waitFor(() => expect(confirm).toBeDisabled(), { timeout: 3000 })
    expect(within(dialog).getByText(sentence)).toHaveTextContent("Confirm waits for it.")

    current = waitingSchema()
    watch.push(invalidate())
    await waitFor(() => expect(confirm).toBeEnabled(), { timeout: 3000 })
  })

  it("fails the full console's check until the read ends", async () => {
    renderConsole("/console/schemas/application/storefront/plan", {
      schemas: [beingRead()],
      migrations: [],
    })

    const checks = await screen.findByRole("list", { name: "Checks before approving" })
    expect(checks).toHaveTextContent("The operator is reading the resource again")
    expect(screen.getByRole("button", { name: "Approve this plan…" })).toBeDisabled()
  })
})
