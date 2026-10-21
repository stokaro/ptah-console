import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { Access, StageState } from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, syncedMigration, waitingSchema } from "../../test/console"

function schemaNamed(name: string, minutes: number) {
  return waitingSchema((schema) => {
    schema.ref!.name = name
    schema.ref!.uid = `uid-${name}`
    schema.attention = []
    for (const stage of schema.stages) {
      stage.state = StageState.DONE
    }
    schema.conditions = [{ ...schema.conditions[0], lastTransition: minutesAgo(minutes) }]
  })
}

function rowNames() {
  const table = screen.getByRole("table")
  return within(table)
    .getAllByRole("row")
    .slice(1)
    .map((row) => within(row).getAllByRole("cell")[0].querySelector(".font-mono")!.textContent)
}

describe("Resources", () => {
  it("lists every resource, what needs a person first", async () => {
    const { container } = renderConsole("/console/resources", {
      schemas: [schemaNamed("inventory", 30), waitingSchema()],
      migrations: [syncedMigration()],
      otherNamespaces: [{ name: "payroll", access: Access.FORBIDDEN }],
    })

    expect(await screen.findByRole("table")).toBeInTheDocument()
    expect(rowNames()).toEqual(["storefront", "billing", "inventory"])
    const storefront = screen.getAllByRole("row")[1]
    expect(within(storefront).getByText("Waiting for approval")).toBeInTheDocument()
    expect(within(storefront).getByRole("link", { name: /storefront/ })).toHaveAttribute(
      "href",
      "/releases/application/schema/storefront"
    )
    expect(
      screen.getByText(/Not shown, because this identity may not read them/)
    ).toHaveTextContent("payroll")
    expect(screen.getByRole("link", { name: /Resources/, current: "page" })).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("filters by kind and state, and keeps the filters in the address", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console/resources", {
      schemas: [schemaNamed("inventory", 30), waitingSchema()],
      migrations: [syncedMigration()],
    })
    await screen.findByRole("table")

    await user.click(screen.getByRole("button", { name: "Schemas 2" }))
    expect(rowNames()).toEqual(["storefront", "inventory"])
    await user.click(screen.getByRole("button", { name: "In sync 1" }))
    expect(rowNames()).toEqual(["inventory"])
    expect(history.location.search).toContain("kind=schema")
    expect(history.location.search).toContain("state=insync")
  })

  it("opens a shared link with its filters", async () => {
    renderConsole("/console/resources?kind=migration", {
      schemas: [waitingSchema()],
      migrations: [syncedMigration()],
    })

    await screen.findByRole("table")
    expect(rowNames()).toEqual(["billing"])
    expect(screen.getByRole("button", { name: "Migrations 1", pressed: true })).toBeInTheDocument()
  })

  it("finds a resource by why it waits", async () => {
    const user = userEvent.setup()
    renderConsole("/console/resources", {
      schemas: [schemaNamed("inventory", 30), waitingSchema()],
    })
    await screen.findByRole("table")

    await user.type(screen.getByRole("textbox", { name: "Search resources" }), "approver")

    expect(rowNames()).toEqual(["storefront"])
  })

  it("says nothing matches, and resets the filters", async () => {
    const user = userEvent.setup()
    renderConsole("/console/resources?state=suspended", { schemas: [waitingSchema()] })

    expect(await screen.findByText("Nothing matches")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Reset filters" }))

    expect(rowNames()).toEqual(["storefront"])
  })

  it("pages through a long list", async () => {
    const user = userEvent.setup()
    const schemas = Array.from({ length: 30 }, (_, index) =>
      schemaNamed(`schema-${String(index).padStart(2, "0")}`, index + 1)
    )
    renderConsole("/console/resources?sort=name", { schemas })
    await screen.findByRole("table")

    expect(screen.getAllByRole("row")).toHaveLength(26)
    expect(screen.getByText("1–25 of 30")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Next →" }))

    expect(rowNames()[0]).toBe("schema-25")
    expect(screen.getByText("26–30 of 30")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Next →" })).toBeDisabled()
  })

  it("is one link away from the focused view, and back", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/releases", { schemas: [waitingSchema()] })

    await user.click(await screen.findByRole("link", { name: "Full console" }))
    expect(await screen.findByRole("table")).toBeInTheDocument()
    expect(history.location.pathname).toBe("/console/resources")

    await user.click(screen.getByRole("link", { name: /Focused view/ }))
    // The focused view's home sends an approver to the queue.
    expect(await screen.findByRole("navigation", { name: "Role" })).toBeInTheDocument()
    expect(history.location.pathname).toBe("/approve")
  })

  it("opens the console's front page on the resources", async () => {
    const { history } = renderConsole("/console", { schemas: [waitingSchema()] })

    expect(await screen.findByRole("table")).toBeInTheDocument()
    expect(history.location.pathname).toBe("/console/resources")
  })
})
