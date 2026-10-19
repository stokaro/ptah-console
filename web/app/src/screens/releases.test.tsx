import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { DecisionOutcome, type SetSuspendedRequest } from "../gen/ptah/console/v1/decisions_pb"
import { renderConsole, waitingSchema } from "../test/console"

describe("Developer", () => {
  it("shows where a release stands, step by step", async () => {
    const { container } = renderConsole("/releases", { schemas: [waitingSchema()] })

    const progress = await screen.findByRole("list", { name: "Progress of storefront" })
    expect(within(progress).getAllByRole("listitem")).toHaveLength(5)
    for (const step of [
      "Resolve: done",
      "Plan: done",
      "Approve: needs attention",
      "Apply: not applicable",
      "Verify: not applicable",
    ]) {
      expect(
        within(progress).getByText((_, element) => element?.textContent === step)
      ).toBeInTheDocument()
    }
    expect(
      screen.getByText("Waiting for an approver. Nothing has changed in the database yet.")
    ).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("suspends the resource at the version the page shows", async () => {
    const user = userEvent.setup()
    const sent: SetSuspendedRequest[] = []
    renderConsole("/releases/application/schema/storefront", {
      schemas: [waitingSchema()],
      setSuspended: (request) => {
        sent.push(request)
        return { outcome: DecisionOutcome.ADMITTED, resourceVersion: "42" }
      },
    })

    expect(
      await screen.findByRole("list", { name: "What happened to storefront" })
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Suspend…" }))
    const dialog = await screen.findByRole("alertdialog")
    await user.click(within(dialog).getByRole("button", { name: "Confirm" }))

    expect(await within(dialog).findByText("Suspended")).toBeInTheDocument()
    expect(sent[0]).toMatchObject({
      name: "storefront",
      uid: "uid-schema",
      resourceVersion: "41",
      suspended: true,
    })
  })
})
