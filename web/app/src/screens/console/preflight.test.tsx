import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { forgetScopeChoices } from "../../api/scope"
import { Access, Answer } from "../../gen/ptah/console/v1/resources_pb"
import { choiceKey } from "../../model/scope"
import { renderConsole } from "../../test/console"

// Every readable namespace is already chosen, so a fixture with several does
// not open the welcome over the page.
beforeEach(() => {
  window.localStorage.setItem(choiceKey("kind-ptah", "alice"), '{"all":true}')
  window.localStorage.setItem(choiceKey("in-cluster", "ptah-console:prod:alex"), '{"all":true}')
})

afterEach(() => {
  window.localStorage.clear()
  forgetScopeChoices()
})

/** The answer one namespace's row gives to one action, as its cell names it. */
function answer(table: HTMLElement, namespace: string, action: string) {
  const row = within(table).getByRole("row", { name: new RegExp(`^${namespace}`) })
  return within(row)
    .getAllByRole("img")
    .map((glyph) => glyph.getAttribute("aria-label"))
    .find((label) => label?.startsWith(`${action}: `))
}

async function matrix() {
  return screen.findByRole("table", { name: "What this identity may do, by namespace" })
}

async function check(title: string) {
  const list = await screen.findByRole("list", { name: "Checks" })
  const item = within(list)
    .getAllByRole("listitem")
    .find((candidate) => within(candidate).queryByText(title, { selector: "b" }) !== null)
  if (!item) {
    throw new Error(`no check named ${title}`)
  }
  return item
}

describe("Pre-flight's matrix", () => {
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

    const table = await matrix()
    expect(answer(table, "application", "List schemas")).toBe("List schemas: Allowed")
    expect(answer(table, "application", "Read a plan's SQL")).toBe("Read a plan's SQL: Denied")
    expect(answer(table, "application", "Approve schema plans")).toBe(
      "Approve schema plans: Unknown"
    )
    // A capability the answer leaves out is unknown, never allowed.
    expect(answer(table, "application", "Read logs")).toBe("Read logs: Unknown")
    expect(answer(table, "payroll", "List schemas")).toBe("List schemas: No access")
    expect(within(table).getAllByRole("img")).toHaveLength(2 * 13)
    expect(await axe(container)).toHaveNoViolations()
  })

  it("says what each column asks the API server", async () => {
    renderConsole("/console/preflight")

    const legend = await screen.findByRole("list", { name: "What each column asks" })
    expect(within(legend).getAllByRole("listitem")).toHaveLength(13)
    expect(legend).toHaveTextContent("Read logs: get pods/log")
    expect(legend).toHaveTextContent("Acknowledge runs: create ptahmigrationrunacknowledgments")
  })

  it("filters the namespaces and shows at most 25 of them", async () => {
    const user = userEvent.setup()
    const others = Array.from({ length: 30 }, (_, i) => ({
      name: `team-${String(i).padStart(2, "0")}`,
      access: Access.OK,
    }))
    renderConsole("/console/preflight", { otherNamespaces: others })

    const table = await matrix()
    expect(within(table).getAllByRole("rowheader")).toHaveLength(25)
    expect(screen.getByText("Showing 25 of 31 · refine the filter")).toBeVisible()
    await user.type(screen.getByRole("textbox", { name: "Filter namespaces" }), "TEAM-2")
    expect(
      within(table)
        .getAllByRole("rowheader")
        .map((header) => header.textContent)
    ).toEqual(Array.from({ length: 10 }, (_, i) => `team-2${i}`))
    expect(screen.queryByText(/refine the filter/)).not.toBeInTheDocument()
    await user.clear(screen.getByRole("textbox", { name: "Filter namespaces" }))
    await user.type(screen.getByRole("textbox", { name: "Filter namespaces" }), "payroll")
    expect(screen.getByText("No namespace matches “payroll”.")).toBeVisible()
  })
})

describe("Pre-flight's checks", () => {
  it("pass when the API is served, the cluster runs a tested minor and every namespace is read", async () => {
    renderConsole("/console/preflight")

    for (const title of ["Operator API", "Kubernetes", "Session", "Namespaces"]) {
      expect(within(await check(title)).getByRole("img", { name: "passed" })).toBeInTheDocument()
    }
    expect(await check("Kubernetes")).toHaveTextContent(
      "v1.36.4, a minor this console was tested on"
    )
    expect(await check("Session")).toHaveTextContent(
      "Acts as alice through the kubeconfig, context kind-ptah."
    )
  })

  it("name the resources the cluster does not serve", async () => {
    renderConsole("/console/preflight", {
      operatorApi: { served: true, missing: ["ptahmigrationrunacknowledgments"] },
    })

    const item = await check("Operator API")
    expect(within(item).getByRole("img", { name: "warning" })).toBeInTheDocument()
    expect(item).toHaveTextContent("Served without ptahmigrationrunacknowledgments.")
  })

  it("fail when the cluster serves no operator API, in its own words", async () => {
    renderConsole("/console/preflight", {
      operatorApi: {
        served: false,
        problem: "This cluster does not serve operator.ptah.run/v1alpha1.",
      },
    })

    const item = await check("Operator API")
    expect(within(item).getByRole("img", { name: "failed" })).toBeInTheDocument()
    expect(item).toHaveTextContent("This cluster does not serve operator.ptah.run/v1alpha1.")
  })

  it("warn about a Kubernetes minor this console was not tested on, and do not judge an unknown one", async () => {
    const { unmount } = renderConsole("/console/preflight", { kubernetesVersion: "v1.38.0" })
    const untested = await check("Kubernetes")
    expect(within(untested).getByRole("img", { name: "warning" })).toBeInTheDocument()
    expect(untested).toHaveTextContent(
      "v1.38.0 is not among the minors this console was tested on: 1.35, 1.36, 1.37."
    )
    unmount()

    renderConsole("/console/preflight", { kubernetesVersion: "" })
    const unknown = await check("Kubernetes")
    expect(within(unknown).getByRole("img", { name: "unknown" })).toBeInTheDocument()
  })

  it("warn about namespaces the identity may not read, a cut list and a read-only console", async () => {
    renderConsole("/console/preflight", {
      otherNamespaces: [{ name: "payroll", access: Access.FORBIDDEN }],
      namespacesTruncated: true,
      readOnly: true,
    })

    expect(await check("Namespaces")).toHaveTextContent(
      "May read 1 of the 2 offered. The identity may list more; the console offers the first 64."
    )
    expect(await check("Session")).toHaveTextContent("read-only: it sends no decision")
  })

  it("ask every question again on Re-run checks", async () => {
    const user = userEvent.setup()
    let asked = 0
    renderConsole("/console/preflight", { onListNamespaces: () => asked++ })
    await matrix()
    const before = asked

    await user.click(screen.getByRole("button", { name: "Re-run checks" }))

    await waitFor(() => expect(asked).toBeGreaterThan(before))
  })
})

describe("Pre-flight's actions and facts", () => {
  it("copies a kubectl command that asks the same of the namespaces shown", async () => {
    const user = userEvent.setup()
    renderConsole("/console/preflight", {
      otherNamespaces: [{ name: "billing", access: Access.OK }],
    })
    await matrix()

    await user.click(screen.getByRole("button", { name: "Copy kubectl command" }))

    expect(await navigator.clipboard.readText()).toBe(
      'for ns in application billing; do echo "== $ns"; kubectl --context kind-ptah auth can-i --list --namespace "$ns"; done'
    )
    expect(screen.getByText("Copied.")).toBeVisible()
  })

  it("asks as the account a hosted console impersonates", async () => {
    const user = userEvent.setup()
    renderConsole("/console/preflight", { hosted: { login: () => {}, signedIn: true } })
    await matrix()

    await user.click(screen.getByRole("button", { name: "Copy kubectl command" }))

    expect(await navigator.clipboard.readText()).toContain(
      "kubectl --as ptah-console:prod:alex auth can-i --list"
    )
  })

  it("asks with the person's own sign-in when the session holds an OpenID Connect token", async () => {
    const user = userEvent.setup()
    renderConsole("/console/preflight", {
      hosted: { login: () => {}, signedIn: true, provider: "Example SSO" },
      username: "oidc:alex@example.com",
    })
    await matrix()

    await user.click(screen.getByRole("button", { name: "Copy kubectl command" }))

    const command = await navigator.clipboard.readText()
    expect(command).toContain('kubectl auth can-i --list --namespace "$ns"')
    expect(command).not.toContain("--as")
    expect(
      screen.getByText(/Acts as oidc:alex@example.com with its OpenID Connect token/)
    ).toBeVisible()
    expect(screen.queryByText(/impersonation/)).not.toBeInTheDocument()
  })

  it("says what this console was built and tested against", async () => {
    renderConsole("/console/preflight")

    const table = await screen.findByRole("table", {
      name: "What this console was built and tested against",
    })
    const row = (name: string) => within(table).getByRole("row", { name: new RegExp(`^${name}`) })
    expect(row("This console")).toHaveTextContent("v0.1.0commit 0123456789ab")
    expect(row("Built against")).toHaveTextContent("Ptah Operator v0.2.5")
    expect(row("Plan contracts")).toHaveTextContent("1, 2")
    expect(row("Tested on")).toHaveTextContent(
      "Kubernetes 1.35, 1.36, 1.37this cluster runs v1.36.4"
    )
  })

  it("says what the console never reads, whatever RBAC allows", async () => {
    renderConsole("/console/preflight")

    expect(await screen.findByRole("region", { name: "Never read" })).toHaveTextContent(
      "never reads Secrets, PtahResultRecord objects"
    )
  })
})
