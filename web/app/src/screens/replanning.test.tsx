import { screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { renderConsole, waitingSchema } from "../test/console"

// What the operator wrote in the demo lab while it planned again: approval
// still required, no current plan. The schema stays in the queue and says
// why it cannot be approved yet, instead of vanishing until the plan is back.
const replanning = () =>
  waitingSchema((schema) => {
    schema.currentPlan = undefined
    schema.phase = "Planning"
    schema.attention[0].nextStep =
      "The operator is planning again. The plan can be approved once it is published; nothing changes in the database until then."
  })

describe("A schema being planned again", () => {
  it("stays in the queue with nothing to approve", async () => {
    renderConsole("/approve", { schemas: [replanning()] })

    const queue = await screen.findByRole("list", { name: "Plans waiting for approval" })
    expect(within(queue).getByRole("link", { name: "storefront" })).toBeInTheDocument()
    expect(within(queue).getByText(/planning again/)).toBeInTheDocument()
    expect(within(queue).queryByRole("button", { name: "Approve…" })).toBeNull()
  })

  it("says the plan comes back, and reads no plan", async () => {
    renderConsole("/approve/application/schema/storefront", { schemas: [replanning()] })

    expect(
      await screen.findByText(/The plan appears here once it is published/)
    ).toBeInTheDocument()
    expect(
      screen.getByText(/Nothing to approve until the operator publishes the plan again/)
    ).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Approve this plan…" })).toBeNull()
    expect(screen.queryByText("Reading the plan's SQL…")).toBeNull()
  })
})
