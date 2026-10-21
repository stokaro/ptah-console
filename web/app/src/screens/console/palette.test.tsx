import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { renderConsole, syncedMigration, waitingSchema } from "../../test/console"

const fixture = { schemas: [waitingSchema()], migrations: [syncedMigration()] }

describe("Command palette", () => {
  it("opens with Ctrl+K, finds a resource by part of its name, and opens it with Enter", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console", fixture)
    await screen.findByRole("heading", { name: "Overview" })

    await user.keyboard("{Control>}k{/Control}")
    const search = await screen.findByRole("combobox", { name: "Search pages and resources" })
    await user.type(search, "bill")
    const options = within(screen.getByRole("dialog")).getAllByRole("option")
    expect(options.map((option) => option.textContent)).toEqual(["✓billingapplication · In sync"])
    await user.keyboard("{Enter}")

    await waitFor(() =>
      expect(history.location.pathname).toBe("/console/migrations/application/billing")
    )
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("finds a resource by its namespace and state, and a page by its name", async () => {
    const user = userEvent.setup()
    renderConsole("/console", fixture)
    await screen.findByRole("heading", { name: "Overview" })

    await user.keyboard("/")
    const search = await screen.findByRole("combobox", { name: "Search pages and resources" })
    await user.type(search, "waiting")
    expect(
      within(screen.getByRole("dialog"))
        .getAllByRole("option")
        .map((o) => o.textContent)
    ).toEqual(["!storefrontapplication · Waiting for approval"])
    await user.clear(search)
    await user.type(search, "pre-fl")
    expect(
      within(screen.getByRole("dialog"))
        .getAllByRole("option")
        .map((o) => o.textContent)
    ).toEqual(["Pre-flightfull console"])
  })

  it("moves with the arrow keys and closes with Escape", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console", fixture)
    await screen.findByRole("heading", { name: "Overview" })

    await user.click(screen.getByRole("button", { name: /Search or jump to/ }))
    const search = await screen.findByRole("combobox", { name: "Search pages and resources" })
    await waitFor(() => expect(search).toHaveFocus())
    await user.keyboard("{ArrowDown}{Enter}")
    await waitFor(() => expect(history.location.pathname).toBe("/console/resources"))

    await user.keyboard("{Control>}k{/Control}")
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })

  it("leaves / to a field being typed in", async () => {
    const user = userEvent.setup()
    renderConsole("/console/resources", fixture)
    const field = await screen.findByRole("textbox", { name: "Search resources" })

    await user.type(field, "a/b")

    expect(field).toHaveValue("a/b")
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("passes an accessibility check while open", async () => {
    const user = userEvent.setup()
    renderConsole("/console", fixture)
    await screen.findByRole("heading", { name: "Overview" })

    await user.keyboard("{Control>}k{/Control}")
    const dialog = await screen.findByRole("dialog")

    expect(await axe(dialog)).toHaveNoViolations()
  })
})
