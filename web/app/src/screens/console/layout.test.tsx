import { timestampFromDate } from "@bufbuild/protobuf/wkt"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { afterEach, describe, expect, it, vi } from "vitest"

import { renderConsole, syncedMigration, waitingSchema } from "../../test/console"
import { initials } from "./Layout"

const hourAgo = () => timestampFromDate(new Date(Date.now() - 3_600_000))
const soon = () => timestampFromDate(new Date(Date.now() + 60_000))

afterEach(() => {
  document.documentElement.removeAttribute("data-accent")
  window.localStorage.clear()
})

describe("The full console's navigation", () => {
  it("groups the pages and counts what needs attention and what it reads", async () => {
    renderConsole("/console", { schemas: [waitingSchema()], migrations: [syncedMigration()] })

    const nav = await screen.findByRole("navigation", { name: "Console" })
    const workspace = within(nav).getByRole("group", { name: "Workspace" })
    const installation = within(nav).getByRole("group", { name: "Installation" })
    expect(
      await within(workspace).findByRole("link", { name: /Resources, 2 resources/ })
    ).toBeVisible()
    expect(
      within(workspace).getByRole("link", { name: /Overview, 1 needs attention/ })
    ).toBeVisible()
    expect(
      within(installation).getByRole("link", { name: "Architecture & safety" })
    ).toHaveAttribute("href", "/console/architecture")
    expect(within(installation).getByRole("link", { name: "Pre-flight" })).toBeVisible()
  })

  it("says the counts in the plural", async () => {
    renderConsole("/console", {
      schemas: [
        waitingSchema(),
        waitingSchema((schema) => {
          schema.ref = { ...schema.ref!, name: "inventory", uid: "uid-inventory" }
        }),
      ],
      migrations: [],
    })

    const nav = await screen.findByRole("navigation", { name: "Console" })
    expect(await within(nav).findByRole("link", { name: "Resources, 2 resources" })).toBeVisible()
    expect(within(nav).getByRole("link", { name: "Overview, 2 need attention" })).toBeVisible()
  })

  it("says a lone resource in the singular", async () => {
    renderConsole("/console", { schemas: [], migrations: [syncedMigration()] })

    const nav = await screen.findByRole("navigation", { name: "Console" })
    expect(await within(nav).findByRole("link", { name: "Resources, 1 resource" })).toBeVisible()
    expect(within(nav).getByRole("link", { name: "Overview, 0 need attention" })).toBeVisible()
  })
})

describe("The operator's schedule", () => {
  it("is kept while every resource is read when promised", async () => {
    renderConsole("/console", {
      schemas: [waitingSchema((schema) => (schema.nextReconciliation = soon()))],
      migrations: [syncedMigration((migration) => (migration.nextReconciliation = soon()))],
    })

    const block = await screen.findByRole("region", { name: "Operator schedule" })
    expect(await within(block).findByText(/on schedule/)).toBeVisible()
    expect(block).toHaveTextContent("2 resources were read when promised")
    expect(block).toHaveTextContent("console built against v0.2.5")
  })

  it("names how many resources are past their read, and the latest", async () => {
    renderConsole("/console", {
      schemas: [waitingSchema((schema) => (schema.nextReconciliation = hourAgo()))],
      migrations: [syncedMigration((migration) => (migration.nextReconciliation = soon()))],
    })

    const block = await screen.findByRole("region", { name: "Operator schedule" })
    expect(await within(block).findByText(/1 past their next read/)).toBeVisible()
    expect(
      within(block).getByRole("link", { name: /Latest: application\/storefront/ })
    ).toBeVisible()
  })

  it("judges nothing when no resource promises a read", async () => {
    renderConsole("/console", {
      schemas: [waitingSchema((schema) => (schema.nextReconciliation = undefined))],
      migrations: [],
    })

    const block = await screen.findByRole("region", { name: "Operator schedule" })
    expect(await within(block).findByText(/nothing to judge by/)).toBeVisible()
  })

  it("says the API is not served before anything about the schedule", async () => {
    renderConsole("/console", {
      operatorApi: { served: false, problem: "The cluster serves no operator.ptah.run API." },
    })

    const block = await screen.findByRole("region", { name: "Operator schedule" })
    expect(await within(block).findByText(/API not served/)).toBeVisible()
    expect(block).toHaveTextContent("The cluster serves no operator.ptah.run API.")
  })
})

describe("The accent", () => {
  it("is the viewer's choice, kept, and leaves the states alone", async () => {
    const user = userEvent.setup()
    renderConsole("/console")

    await user.click(await screen.findByRole("button", { name: /Accent color: Indigo/ }))
    await user.click(await screen.findByRole("menuitemradio", { name: "Teal" }))

    expect(document.documentElement).toHaveAttribute("data-accent", "teal")
    expect(window.localStorage.getItem("ptah-console-accent")).toBe("teal")

    await user.click(screen.getByRole("button", { name: /Accent color: Teal/ }))
    await user.click(await screen.findByRole("menuitemradio", { name: "Indigo" }))

    expect(document.documentElement).not.toHaveAttribute("data-accent")
    expect(window.localStorage.getItem("ptah-console-accent")).toBeNull()
  })

  it("still changes when the browser refuses storage", async () => {
    const user = userEvent.setup()
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked")
    })
    renderConsole("/console")

    await user.click(await screen.findByRole("button", { name: /Accent color/ }))
    await user.click(await screen.findByRole("menuitemradio", { name: "Graphite" }))

    expect(document.documentElement).toHaveAttribute("data-accent", "graphite")
    vi.restoreAllMocks()
  })
})

describe("initials", () => {
  it.each([
    ["Alex Martin", "AM"],
    ["alex.martin@example.com", "AM"],
    ["kubernetes-admin", "KA"],
    ["system:serviceaccount:application:demo-approver", "DA"],
    ["root", "RO"],
    ["", "?"],
  ])("of %j is %j", (name, want) => {
    expect(initials(name)).toBe(want)
  })
})

describe("Architecture & safety", () => {
  it("says how the operator works, links its docs, and meets the accessibility checks", async () => {
    const { container } = renderConsole("/console/architecture")

    expect(
      await screen.findByRole("heading", { level: 1, name: "Architecture & safety" })
    ).toBeVisible()
    for (const name of [
      "System map",
      "Operator lifecycle",
      "Key safety properties",
      "Two kinds of resource",
      "Security boundaries",
    ]) {
      expect(screen.getByRole("region", { name })).toBeVisible()
    }
    expect(screen.getByRole("link", { name: "security" })).toHaveAttribute(
      "href",
      "https://operator.ptah.run/use/security/"
    )
    expect(await axe(container)).toHaveNoViolations()
  })
})
