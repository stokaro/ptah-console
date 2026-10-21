import { screen, within } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { Access, Answer } from "../../gen/ptah/console/v1/resources_pb"
import { renderConsole } from "../../test/console"

function answer(table: HTMLElement, action: string, namespace: string) {
  const columns = within(table)
    .getAllByRole("columnheader")
    .map((h) => h.textContent)
  const row = within(table).getByRole("row", { name: new RegExp(`^${action}`) })
  return within(row).getAllByRole("cell")[columns.indexOf(namespace) - 1].textContent
}

describe("Pre-flight", () => {
  it("answers each action in each namespace, unknown as unknown and an unreadable namespace as no access", async () => {
    const { container } = renderConsole("/console/preflight", {
      capabilities: {
        listSchemas: Answer.ALLOWED,
        listMigrations: Answer.ALLOWED,
        readPlanSql: Answer.DENIED,
        approveSchemaPlans: Answer.UNKNOWN,
      },
      otherNamespaces: [{ name: "payroll", access: Access.FORBIDDEN }],
    })

    const table = await screen.findByRole("table", {
      name: "What this identity may do, by namespace",
    })
    expect(answer(table, "List schemas", "application")).toBe("✓Allowed")
    expect(answer(table, "Read a plan's SQL", "application")).toBe("–Denied")
    expect(answer(table, "Approve schema plans", "application")).toBe("?Unknown")
    // A capability the answer leaves out is unknown, never allowed.
    expect(answer(table, "Read logs", "application")).toBe("?Unknown")
    expect(answer(table, "List schemas", "payroll")).toBe("–No access")
    expect(await axe(container)).toHaveNoViolations()
  })

  it("names the operator the console was built against and whether the API is served", async () => {
    renderConsole("/console/preflight")

    const operator = await screen.findByRole("region", { name: "Operator" })
    expect(operator).toHaveTextContent("operator.ptah.run/v1alpha1 is served")
    expect(operator).toHaveTextContent("Ptah Operator v0.2.5")
  })

  it("names the resources the cluster does not serve", async () => {
    renderConsole("/console/preflight", {
      operatorApi: {
        served: true,
        missing: ["ptahmigrationrunacknowledgments"],
        problem:
          "This cluster serves operator.ptah.run/v1alpha1 without every resource the console needs.",
      },
    })

    const operator = await screen.findByRole("region", { name: "Operator" })
    expect(operator).toHaveTextContent("without every resource the console needs")
    expect(operator).toHaveTextContent("ptahmigrationrunacknowledgments")
  })

  it("says what the console never reads, whatever RBAC allows", async () => {
    renderConsole("/console/preflight")

    expect(await screen.findByRole("region", { name: "Never read" })).toHaveTextContent(
      "never reads Secrets, PtahResultRecord objects"
    )
  })
})
