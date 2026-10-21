import { create } from "@bufbuild/protobuf"
import { screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import {
  Access,
  AttentionKind,
  AttentionSchema,
  StageState,
} from "../../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, syncedMigration, waitingSchema } from "../../test/console"

const failedMigration = syncedMigration((migration) => {
  migration.ref!.name = "journal"
  migration.ref!.uid = "uid-journal"
  migration.phase = "Failed"
  migration.stages[4].state = StageState.FAILED
  migration.attention = [
    create(AttentionSchema, {
      kind: AttentionKind.FAILED,
      reason: "Migration 2 (Read Ledger) failed and committed nothing.",
      since: minutesAgo(3),
    }),
  ]
  migration.conditions = [
    {
      ...migration.conditions[0],
      type: "ReconciliationFailed",
      status: "True",
      reason: "MigrationFailed",
      message: "Migration 2 (Read Ledger) failed and committed nothing.",
      lastTransition: minutesAgo(3),
    },
  ]
})

const fixture = {
  schemas: [waitingSchema()],
  migrations: [syncedMigration(), failedMigration],
  otherNamespaces: [{ name: "payroll", access: Access.FORBIDDEN }],
}

function attentionNames() {
  const table = screen.getByRole("table", { name: "Resources that need attention" })
  return within(table)
    .getAllByRole("link")
    .map((link) => link.querySelector(".font-mono")!.textContent)
}

describe("Overview", () => {
  it("counts each state and links each count to the resources in it", async () => {
    const { container } = renderConsole("/console", fixture)

    const states = await screen.findByRole("navigation", { name: "Resources by state" })
    const attention = within(states).getByRole("link", { name: /Needs attention/ })
    expect(attention).toHaveTextContent("2")
    expect(attention).toHaveAttribute("href", "/console/resources?state=attention")
    expect(within(states).getByRole("link", { name: /In sync/ })).toHaveTextContent("1")
    expect(screen.getByRole("img", { name: "Needs attention: 2, In sync: 1" })).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("puts a failure before a plan waiting for approval", async () => {
    renderConsole("/console", fixture)

    await screen.findByRole("table", { name: "Resources that need attention" })
    expect(attentionNames()).toEqual(["journal", "storefront"])
  })

  it("narrows the attention table to one kind", async () => {
    const user = userEvent.setup()
    renderConsole("/console", fixture)
    await screen.findByRole("table", { name: "Resources that need attention" })

    await user.click(screen.getByRole("button", { name: "Approval 1" }))

    expect(attentionNames()).toEqual(["storefront"])
  })

  it("moves through the attention table with j and k, and opens a row with Enter", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole("/console", fixture)
    await screen.findByRole("table", { name: "Resources that need attention" })
    const [journal, storefront] = within(
      screen.getByRole("table", { name: "Resources that need attention" })
    ).getAllByRole("link")

    await user.keyboard("j")
    expect(journal).toHaveFocus()
    await user.keyboard("j")
    expect(storefront).toHaveFocus()
    await user.keyboard("j")
    expect(storefront).toHaveFocus()
    await user.keyboard("k")
    expect(journal).toHaveFocus()
    await user.keyboard("{Enter}")

    expect(history.location.pathname).toBe("/console/migrations/application/journal")
  })

  it("leaves j and k to a field being typed in", async () => {
    const user = userEvent.setup()
    renderConsole("/console", fixture)
    await screen.findByRole("table", { name: "Resources that need attention" })
    const field = document.createElement("input")
    document.body.append(field)
    field.focus()

    await user.keyboard("j")

    expect(field).toHaveFocus()
    expect(field).toHaveValue("j")
    field.remove()
  })

  it("says nothing needs a person when nothing does", async () => {
    renderConsole("/console", { migrations: [syncedMigration()] })

    expect(await screen.findByText("Nothing needs a person right now.")).toBeInTheDocument()
  })

  it("counts each namespace, and names one it may not read", async () => {
    renderConsole("/console", fixture)

    const health = await screen.findByRole("table", { name: "Resource states by namespace" })
    const application = within(health).getByRole("row", { name: /^application/ })
    expect(
      within(application)
        .getAllByRole("cell")
        .map((cell) => cell.textContent)
    ).toEqual(["2", "0", "1", "0", "3"])
    expect(within(health).getByRole("row", { name: /^payroll/ })).toHaveTextContent("No access")
  })

  it("lists the newest condition changes", async () => {
    renderConsole("/console", fixture)

    const activity = await screen.findByRole("region", { name: "Recent activity" })
    const items = within(activity).getAllByRole("listitem")
    expect(items[0]).toHaveTextContent("journal ReconciliationFailed True · MigrationFailed")
    expect(items).toHaveLength(4)
  })
})
