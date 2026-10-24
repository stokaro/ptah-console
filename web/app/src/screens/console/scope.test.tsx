import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { afterEach, describe, expect, it, vi } from "vitest"

import { forgetScopeChoices } from "../../api/scope"
import { ChannelState, ConnectionProfile } from "../../gen/ptah/console/v1/connections_pb"
import { Access } from "../../gen/ptah/console/v1/resources_pb"
import { choiceKey } from "../../model/scope"
import { renderConsole, waitingSchema, type Fixture } from "../../test/console"

const aliceKey = choiceKey("kind-ptah", "alice")

afterEach(() => {
  window.localStorage.clear()
  forgetScopeChoices()
  document.documentElement.removeAttribute("data-accent")
  vi.restoreAllMocks()
})

/** Two readable namespaces with a schema each, and the list requests they answer. */
function twoNamespaces(change: (fixture: Fixture) => void = () => {}) {
  const fixture: Fixture = {
    otherNamespaces: [{ name: "billing", access: Access.OK }],
    schemas: [
      waitingSchema(),
      waitingSchema((schema) => {
        schema.ref = { ...schema.ref!, namespace: "billing", name: "ledger", uid: "uid-ledger" }
      }),
    ],
    migrations: [],
    listed: [],
  }
  change(fixture)
  return fixture
}

/** The namespaces the newest list of schemas named. */
function lastListed(fixture: Fixture) {
  const schemas = (fixture.listed ?? []).filter((request) => request.method === "listSchemas")
  return schemas.at(-1)?.namespaces
}

const welcomeName = "Choose where you want to work"

describe("The first visit to the full console", () => {
  it("asks where to work, keeps the choice, and reads only what was chosen", async () => {
    const user = userEvent.setup()
    const fixture = twoNamespaces()
    renderConsole("/console", fixture)

    const welcome = await screen.findByRole("dialog", { name: welcomeName })
    expect(welcome).toHaveTextContent("Step 1 of 2")
    const list = within(welcome).getByRole("list", { name: "Namespaces" })
    await user.click(within(list).getByRole("button", { name: /application/ }))
    expect(within(list).getByRole("button", { name: /application/ })).toHaveAttribute(
      "aria-pressed",
      "false"
    )
    expect(within(welcome).getAllByText("1 of 2 readable selected").length).toBeGreaterThan(0)
    await user.click(within(welcome).getByRole("button", { name: "Continue" }))
    expect(welcome).toHaveTextContent("Step 2 of 2")
    await user.click(within(welcome).getByRole("button", { name: "Open the console" }))

    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
    )
    expect(window.localStorage.getItem(aliceKey)).toBe('{"all":false,"namespaces":["billing"]}')
    await waitFor(() => expect(lastListed(fixture)).toEqual(["billing"]))
    expect(await screen.findByText(/in 1 of the 2 namespaces this identity may read/)).toBeVisible()
    expect(
      screen.getByRole("button", { name: "Namespaces: 1 of 2 readable shown. Choose" })
    ).toHaveTextContent("billing")
  })

  it("does not ask when there is one namespace to read", async () => {
    renderConsole("/console", { schemas: [waitingSchema()], migrations: [] })

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeVisible()
    expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
  })

  it("does not ask again once the viewer chose", async () => {
    window.localStorage.setItem(aliceKey, '{"all":false,"namespaces":["billing"]}')
    const fixture = twoNamespaces()
    renderConsole("/console", fixture)

    await waitFor(() => expect(lastListed(fixture)).toEqual(["billing"]))
    expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
  })

  it("reads nothing until the session says whose choice to read", async () => {
    window.localStorage.setItem(aliceKey, '{"all":false,"namespaces":["billing"]}')
    let answer = () => {}
    let scopeRead = false
    const fixture = twoNamespaces((fixture) => {
      fixture.sessionAnswered = new Promise((resolve) => (answer = resolve))
      fixture.onListNamespaces = () => (scopeRead = true)
    })
    renderConsole("/console", fixture)

    // The namespaces are known and the session is not: every readable
    // namespace would be read if the console did not wait.
    await waitFor(() => expect(scopeRead).toBe(true))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(fixture.listed).toEqual([])
    answer()

    await waitFor(() => expect(lastListed(fixture)).toEqual(["billing"]))
    expect(fixture.listed?.every((request) => request.namespaces.join() === "billing")).toBe(true)
  })

  it("asks again on the next visit when it was closed unfinished, and reads everything meanwhile", async () => {
    const user = userEvent.setup()
    const fixture = twoNamespaces()
    const { unmount } = renderConsole("/console", fixture)

    await screen.findByRole("dialog", { name: welcomeName })
    await user.keyboard("{Escape}")

    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
    )
    await waitFor(() => expect(lastListed(fixture)).toEqual(["application", "billing"]))
    expect(window.localStorage.getItem(aliceKey)).toBeNull()
    unmount()

    renderConsole("/console", twoNamespaces())
    expect(await screen.findByRole("dialog", { name: welcomeName })).toBeVisible()
  })

  it("keeps a choice per identity, so another person's is not this one's", async () => {
    window.localStorage.setItem(
      choiceKey("kind-ptah", "bob"),
      '{"all":false,"namespaces":["billing"]}'
    )
    renderConsole("/console", twoNamespaces())

    expect(await screen.findByRole("dialog", { name: welcomeName })).toBeVisible()
  })

  it("still narrows the pages when the browser refuses storage", async () => {
    const user = userEvent.setup()
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked")
    })
    const fixture = twoNamespaces()
    renderConsole("/console", fixture)

    const welcome = await screen.findByRole("dialog", { name: welcomeName })
    await user.click(within(welcome).getByRole("button", { name: /^billing/ }))
    await user.click(within(welcome).getByRole("button", { name: "Continue" }))
    await user.click(within(welcome).getByRole("button", { name: "Open the console" }))

    await waitFor(() => expect(lastListed(fixture)).toEqual(["application"]))
  })

  it("refuses to go on with no namespace chosen", async () => {
    const user = userEvent.setup()
    renderConsole("/console", twoNamespaces())

    const welcome = await screen.findByRole("dialog", { name: welcomeName })
    await user.click(within(welcome).getByRole("button", { name: "Clear" }))

    expect(within(welcome).getByRole("button", { name: "Continue" })).toBeDisabled()
  })

  it("sets the theme and the accent, and the top bar shows the same", async () => {
    const user = userEvent.setup()
    renderConsole("/console", twoNamespaces())

    const welcome = await screen.findByRole("dialog", { name: welcomeName })
    await user.click(within(welcome).getByRole("button", { name: "Continue" }))
    await user.click(
      within(within(welcome).getByRole("radiogroup", { name: "Theme" })).getByRole("radio", {
        name: "Dark",
      })
    )
    await user.click(
      within(within(welcome).getByRole("radiogroup", { name: "Accent" })).getByRole("radio", {
        name: "Teal",
      })
    )
    await user.click(within(welcome).getByRole("button", { name: "Open the console" }))

    expect(document.documentElement).toHaveAttribute("data-theme", "dark")
    expect(document.documentElement).toHaveAttribute("data-accent", "teal")
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
    )
    expect(
      within(screen.getByRole("radiogroup", { name: "Theme" })).getByRole("radio", { name: "Dark" })
    ).toHaveAttribute("aria-checked", "true")
    expect(screen.getByRole("button", { name: /Accent color: Teal/ })).toBeVisible()
  })

  it("meets the accessibility checks", async () => {
    renderConsole("/console", twoNamespaces())

    const welcome = await screen.findByRole("dialog", { name: welcomeName })
    expect(await axe(welcome)).toHaveNoViolations()
  })
})

describe("The namespaces in the top bar", () => {
  it("change what the pages read when the viewer applies them", async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(aliceKey, '{"all":true}')
    const fixture = twoNamespaces()
    renderConsole("/console", fixture)
    await waitFor(() => expect(lastListed(fixture)).toEqual(["application", "billing"]))

    await user.click(screen.getByRole("button", { name: /^Namespaces: 2 of 2 readable shown/ }))
    const popup = await screen.findByRole("dialog", { name: "Namespaces to show" })
    await user.click(within(popup).getByRole("button", { name: /^application/ }))
    await user.click(within(popup).getByRole("button", { name: "Show 1 namespace" }))

    await waitFor(() => expect(lastListed(fixture)).toEqual(["billing"]))
    expect(window.localStorage.getItem(aliceKey)).toBe('{"all":false,"namespaces":["billing"]}')
  })

  it("change nothing when the viewer cancels, and never apply none", async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(aliceKey, '{"all":true}')
    const fixture = twoNamespaces()
    renderConsole("/console", fixture)

    await user.click(await screen.findByRole("button", { name: /^Namespaces: 2 of 2/ }))
    const popup = await screen.findByRole("dialog", { name: "Namespaces to show" })
    await user.click(within(popup).getByRole("button", { name: "Clear" }))
    expect(within(popup).getByRole("button", { name: "Choose at least one" })).toBeDisabled()
    await user.click(within(popup).getByRole("button", { name: "Cancel" }))

    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Namespaces to show" })).not.toBeInTheDocument()
    )
    expect(window.localStorage.getItem(aliceKey)).toBe('{"all":true}')
    expect(lastListed(fixture)).toEqual(["application", "billing"])
  })

  it("list a namespace the identity may not read, unselectable, with the reason", async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(aliceKey, '{"all":true}')
    renderConsole(
      "/console",
      twoNamespaces((fixture) => {
        fixture.otherNamespaces = [
          { name: "billing", access: Access.OK },
          { name: "payroll", access: Access.FORBIDDEN },
        ]
      })
    )

    await user.click(await screen.findByRole("button", { name: /^Namespaces: 2 of 2/ }))
    const popup = await screen.findByRole("dialog", { name: "Namespaces to show" })
    const payroll = within(popup).getByRole("button", { name: /^payroll/ })
    expect(payroll).toBeDisabled()
    expect(payroll).toHaveTextContent("may not read")
    expect(popup).toHaveTextContent("2 of 2 readable selected")
    expect(await axe(popup)).toHaveNoViolations()
  })

  it("search, select what matches and show only what is selected", async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(aliceKey, '{"all":false,"namespaces":["application"]}')
    renderConsole(
      "/console",
      twoNamespaces((fixture) => {
        fixture.otherNamespaces = [
          { name: "billing", access: Access.OK },
          { name: "billing-eu", access: Access.OK },
        ]
      })
    )

    await user.click(await screen.findByRole("button", { name: /^Namespaces: 1 of 3/ }))
    const popup = await screen.findByRole("dialog", { name: "Namespaces to show" })
    await user.type(within(popup).getByRole("textbox", { name: "Search namespaces" }), "BILL")
    const list = within(popup).getByRole("list", { name: "Namespaces" })
    expect(
      within(list)
        .getAllByRole("button")
        .map((button) => button.textContent)
    ).toEqual([expect.stringContaining("billing"), expect.stringContaining("billing-eu")])
    await user.click(within(popup).getByRole("button", { name: "Select 2 matching" }))
    expect(popup).toHaveTextContent("3 of 3 readable selected")
    await user.clear(within(popup).getByRole("textbox", { name: "Search namespaces" }))
    await user.click(within(popup).getByRole("button", { name: /^application/ }))
    await user.click(within(popup).getByRole("button", { name: "Selected only" }))
    expect(
      within(list)
        .getAllByRole("button")
        .map((button) => button.textContent)
    ).toEqual([expect.stringContaining("billing"), expect.stringContaining("billing-eu")])
    await user.type(within(popup).getByRole("textbox", { name: "Search namespaces" }), "payroll")
    expect(within(popup).getByText("No namespace matches “payroll”.")).toBeVisible()
  })

  it("say when a choice lapsed, and show every readable namespace instead of none", async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(aliceKey, '{"all":false,"namespaces":["reports"]}')
    const fixture = twoNamespaces()
    renderConsole("/console", fixture)

    await waitFor(() => expect(lastListed(fixture)).toEqual(["application", "billing"]))
    expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: /^Namespaces: 2 of 2/ }))
    expect(
      await screen.findByText(/None of the namespaces chosen before is readable now/)
    ).toBeVisible()
  })

  it("say the console cut the list, and how to name the namespaces instead", async () => {
    const user = userEvent.setup()
    window.localStorage.setItem(aliceKey, '{"all":true}')
    renderConsole(
      "/console",
      twoNamespaces((fixture) => (fixture.namespacesTruncated = true))
    )

    await user.click(await screen.findByRole("button", { name: /^Namespaces: 2 of 2/ }))
    const popup = await screen.findByRole("dialog", { name: "Namespaces to show" })
    expect(popup).toHaveTextContent(
      "This identity may list more namespaces than the console offers. It offers the first 64 by name; start ptah-console local with --namespace for each one to work in."
    )
  })
})

describe("The focused view", () => {
  it("reads every readable namespace, whatever the full console was narrowed to", async () => {
    window.localStorage.setItem(aliceKey, '{"all":false,"namespaces":["billing"]}')
    const fixture = twoNamespaces()
    renderConsole("/releases", fixture)

    await waitFor(() => expect(lastListed(fixture)).toEqual(["application", "billing"]))
    expect(screen.queryByRole("dialog", { name: welcomeName })).not.toBeInTheDocument()
  })
})

describe("The welcome with more than one cluster", () => {
  const thisCluster = "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f"
  const lab = "1c0b9a8d-7e6f-4b5a-9d3e-2f1a0b9c8d7e"

  it("asks for the cluster first, then that cluster's namespaces, and keeps the choice for it", async () => {
    const user = userEvent.setup()
    const fixture = twoNamespaces((fixture) => {
      fixture.hosted = { login: () => {}, signedIn: true }
      fixture.registry = {
        thisCluster,
        manage: false,
        connections: [
          {
            name: "this-cluster",
            connectionUid: thisCluster,
            profile: ConnectionProfile.CO_LOCATED,
            namespaces: ["application", "billing"],
            enabled: true,
            channel: ChannelState.IN_CLUSTER,
          },
          {
            name: "edge-lab",
            connectionUid: lab,
            profile: ConnectionProfile.AGENT,
            namespaces: ["team-a", "team-b"],
            enabled: true,
            channel: ChannelState.CONNECTED,
            generation: 1n,
          },
        ],
        clusters: {
          [lab]: {
            namespaces: [
              { name: "team-a", access: Access.OK },
              { name: "team-b", access: Access.OK },
            ],
            operatorApi: { served: true },
          },
        },
        calls: [],
        headers: [],
      }
    })
    const { history } = renderConsole("/console", fixture)

    const welcome = await screen.findByRole("dialog", { name: welcomeName })
    expect(welcome).toHaveTextContent("Step 1 of 3")
    await user.click(
      within(within(welcome).getByRole("list", { name: "Clusters" })).getByRole("button", {
        name: /edge-lab/,
      })
    )
    await waitFor(() => expect(history.location.search).toContain(`cluster=${lab}`))
    await user.click(within(welcome).getByRole("button", { name: "Continue" }))
    const list = await within(welcome).findByRole("list", { name: "Namespaces" })
    expect(within(list).getByRole("button", { name: /^team-a/ })).toBeVisible()
    await user.click(within(list).getByRole("button", { name: /^team-b/ }))
    await user.click(within(welcome).getByRole("button", { name: "Continue" }))
    await user.click(within(welcome).getByRole("button", { name: "Open the console" }))

    expect(window.localStorage.getItem(choiceKey(lab, "ptah-console:prod:alex"))).toBe(
      '{"all":false,"namespaces":["team-a"]}'
    )
    expect(window.localStorage.getItem(choiceKey(thisCluster, "ptah-console:prod:alex"))).toBeNull()
  })
})
