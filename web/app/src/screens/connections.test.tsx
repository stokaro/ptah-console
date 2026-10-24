import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it, vi } from "vitest"

import { registryRefreshMs } from "../api/cluster"
import { ChannelState, ConnectionProfile } from "../gen/ptah/console/v1/connections_pb"
import { Access } from "../gen/ptah/console/v1/resources_pb"
import { minutesAgo, renderConsole, waitingSchema, type RegistryFixture } from "../test/console"

const thisCluster = "3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f"
const tokyo = "0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d"
const lab = "1c0b9a8d-7e6f-4b5a-9d3e-2f1a0b9c8d7e"
const vault = "2d1c0b9e-8f70-4c6b-ae4f-3a2b1c0d9e8f"

/** A hosted console with this cluster and three external ones, in their four states. */
function registry(change: (fixture: RegistryFixture) => void = () => {}): RegistryFixture {
  const fixture: RegistryFixture = {
    thisCluster,
    manage: true,
    connections: [
      {
        name: "this-cluster",
        connectionUid: thisCluster,
        profile: ConnectionProfile.CO_LOCATED,
        namespaces: ["application"],
        enabled: true,
        channel: ChannelState.IN_CLUSTER,
        gitops: true,
      },
      {
        name: "prod-ap-2",
        connectionUid: tokyo,
        profile: ConnectionProfile.AGENT,
        namespaces: ["application", "billing"],
        enabled: true,
        channel: ChannelState.DISCONNECTED,
        lastObservedAt: minutesAgo(14),
        generation: 1n,
      },
      {
        name: "edge-lab",
        connectionUid: lab,
        profile: ConnectionProfile.AGENT,
        namespaces: ["application"],
        enabled: true,
        channel: ChannelState.CONNECTED,
        generation: 1n,
      },
      {
        name: "vault",
        connectionUid: vault,
        profile: ConnectionProfile.AGENT,
        namespaces: ["application"],
        enabled: true,
        channel: ChannelState.QUARANTINED,
        generation: 3n,
      },
    ],
    clusters: {
      [tokyo]: "unavailable",
      [lab]: {
        namespaces: [{ name: "application", access: Access.FORBIDDEN, reason: "forbidden" }],
        operatorApi: { served: true },
      },
    },
    calls: [],
    headers: [],
  }
  change(fixture)
  return fixture
}

function hosted(fixture: RegistryFixture) {
  return { hosted: { login: () => {}, signedIn: true }, registry: fixture }
}

describe("The clusters of a hosted console", () => {
  it("switches the cluster a page shows, and every request then names it", async () => {
    const user = userEvent.setup()
    const fixture = registry()
    const { history } = renderConsole("/approve", {
      ...hosted(fixture),
      schemas: [waitingSchema()],
    })

    await user.click(await screen.findByRole("button", { name: /^Cluster: this-cluster/ }))
    await user.click(await screen.findByRole("menuitem", { name: /edge-lab/ }))

    await waitFor(() => expect(history.location.search).toContain(`cluster=${lab}`))
    await waitFor(() => expect(fixture.headers).toContain(lab))
    expect(await screen.findByRole("button", { name: /^Cluster: edge-lab/ })).toBeInTheDocument()
  })

  it("names no cluster the address gives that is not a connection UID", async () => {
    const fixture = registry()
    renderConsole("/approve?cluster=..%2Fthis-cluster", hosted(fixture))

    expect(
      await screen.findByRole("button", { name: /^Cluster: this-cluster/ })
    ).toBeInTheDocument()
    await waitFor(() => expect(fixture.headers.length).toBeGreaterThan(0))
    expect(fixture.headers.every((header) => header === "")).toBe(true)
  })

  it("keeps the cluster across pages, so a link names its cluster", async () => {
    const user = userEvent.setup()
    const { history } = renderConsole(`/approve?cluster=${lab}`, hosted(registry()))

    await user.click(await screen.findByRole("link", { name: "Developer" }))

    await waitFor(() => expect(history.location.pathname).toBe("/releases"))
    expect(history.location.search).toContain(`cluster=${lab}`)
  })

  it("says a disconnected cluster is out of reach, and when it was last heard from", async () => {
    renderConsole(`/approve?cluster=${tokyo}`, hosted(registry()))

    const banner = await screen.findByText(/prod-ap-2 is not connected/)
    expect(banner).toHaveTextContent("last heard from it 14 min ago")
    expect(banner).toHaveTextContent("no decision is sent to it until its agent is back")
  })

  it("offers the Admin role only to who may change the registry", async () => {
    const { unmount } = renderConsole("/approve", hosted(registry()))
    expect(await screen.findByRole("link", { name: "Admin" })).toBeInTheDocument()
    unmount()

    renderConsole("/approve", hosted(registry((fixture) => (fixture.manage = false))))
    await screen.findByRole("link", { name: "Approver" })
    expect(screen.queryByRole("link", { name: "Admin" })).toBeNull()
  })
})

describe("The Admin role", () => {
  it("shows each connection apart: disconnected is not failed, and inaccessible is not empty", async () => {
    const { container } = renderConsole("/connections", hosted(registry()))

    const list = await screen.findByRole("list", { name: "Connections" })
    expect(
      await within(list).findByRole("link", { name: /prod-ap-2 · Disconnected/ })
    ).toBeInTheDocument()
    expect(within(list).getByRole("link", { name: /vault · Quarantined/ })).toBeInTheDocument()
    expect(
      await within(list).findByRole("link", { name: /edge-lab · Connected, no access/ })
    ).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("says what it sees of a connection, dimension by dimension", async () => {
    const { container } = renderConsole("/connections/edge-lab", hosted(registry()))

    expect(await screen.findByText("No Ptah permissions for you here")).toBeInTheDocument()
    expect(screen.getByText("None readable: application")).toBeInTheDocument()
    expect(screen.getByText("The operator's API is served")).toBeInTheDocument()
    expect(screen.getByText(/Ask the namespace's owner to bind a Ptah role/)).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("follows an agent that connects after the page was read", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const fixture = registry((fixture) => {
        fixture.connections.find((c) => c.connectionUid === lab)!.channel =
          ChannelState.NEVER_CONNECTED
      })
      renderConsole("/connections/edge-lab", hosted(fixture))
      expect(
        await screen.findByRole("heading", { level: 1, name: /Awaiting the agent/ })
      ).toBeVisible()
      // Every reader of the registry is mounted, the lazy cluster switcher
      // included, so a read from here on is the page's own refresh.
      expect(await screen.findByRole("button", { name: /^Cluster: / })).toBeVisible()
      await vi.advanceTimersByTimeAsync(0)

      // The agent dials in after the page read the registry; nothing else
      // tells the page, so it has to read the registry again on its own.
      fixture.connections.find((c) => c.connectionUid === lab)!.channel = ChannelState.CONNECTED
      await vi.advanceTimersByTimeAsync(registryRefreshMs)

      expect(await screen.findByText("No Ptah permissions for you here")).toBeInTheDocument()
      expect(
        screen.queryByRole("heading", { level: 1, name: /Awaiting the agent/ })
      ).not.toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it("quarantines a connection with a reason, after asking", async () => {
    const user = userEvent.setup()
    const fixture = registry()
    renderConsole("/connections/edge-lab", hosted(fixture))

    await user.click(await screen.findByRole("button", { name: "Quarantine…" }))
    const dialog = await screen.findByRole("alertdialog")
    expect(dialog).toHaveTextContent("refuses its credentials, even if its record is applied again")
    await user.type(within(dialog).getByLabelText("Reason, for the record"), "credential leaked")
    await user.click(within(dialog).getByRole("button", { name: "Quarantine" }))

    await waitFor(() =>
      expect(fixture.calls).toEqual([
        {
          method: "quarantineConnection",
          request: expect.objectContaining({
            clusterName: "edge-lab",
            reason: "credential leaked",
          }),
        },
      ])
    )
  })

  it("removes a connection and leaves its cleanup pending, with the steps", async () => {
    const user = userEvent.setup()
    const fixture = registry()
    renderConsole("/connections/prod-ap-2", hosted(fixture))

    await user.click(await screen.findByRole("button", { name: "Remove…" }))
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" })
    )

    expect(await screen.findByText(/The cleanup on the target is pending/)).toBeInTheDocument()
    expect(screen.getByText(/helm uninstall ptah-console-agent/)).toBeInTheDocument()
  })

  it("lifts a quarantine with a credential shown once, at the new generation", async () => {
    const user = userEvent.setup()
    renderConsole("/connections/vault", hosted(registry()))

    await user.click(await screen.findByRole("button", { name: "Lift the quarantine…" }))
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Lift" })
    )

    expect(await screen.findByText(/shown this once/)).toHaveTextContent("with generation 2")
    expect(screen.getByText(/--from-literal=credential=shown-once-credential/)).toBeInTheDocument()
  })

  it("adds a cluster: the enrollment file, then the fingerprint the helper printed", async () => {
    const user = userEvent.setup()
    const fixture = registry((registry) => {
      registry.created = {
        enrollmentFile: new TextEncoder().encode("kind: EnrollmentFile\n"),
        fingerprint: "sha256:7f3a91c20bd4e86f00000000000000000000000000000000000000000000abcd",
        connectionUid: "4e3d2c1b-0a9f-4e8d-bc7b-6a5f4e3d2c1b",
        expiresAt: minutesAgo(-10),
      }
    })
    const { container } = renderConsole("/connections/new", hosted(fixture))

    expect(
      await screen.findByText(/A compromised agent can use those accounts' rights/)
    ).toBeInTheDocument()
    await user.type(await screen.findByLabelText("Name"), "payments-eu")
    await user.type(
      screen.getByLabelText("Namespaces it covers, separated by commas"),
      "payments, ledger"
    )
    await user.type(screen.getByLabelText("Account ID"), "alex")
    await user.click(screen.getByRole("checkbox", { name: "approver" }))
    expect(await axe(container)).toHaveNoViolations()
    await user.click(screen.getByRole("button", { name: "Create the enrollment" }))

    expect(
      await screen.findByRole("button", { name: "Download payments-eu.enroll" })
    ).toBeInTheDocument()
    expect(fixture.calls).toEqual([
      {
        method: "createEnrollment",
        request: expect.objectContaining({
          clusterName: "payments-eu",
          namespaces: ["payments", "ledger"],
          accounts: [expect.objectContaining({ id: "alex", roles: ["viewer", "approver"] })],
        }),
      },
    ])
    const done = screen.getByRole("button", { name: "Done" })
    expect(done).toBeDisabled()
    await user.click(screen.getByRole("checkbox", { name: "It matches what the helper printed." }))
    expect(done).toBeEnabled()
  })

  it("activates an enrollment once its agent connected, and only then", async () => {
    const user = userEvent.setup()
    const fixture = registry((registry) => {
      registry.enrollments = [
        {
          clusterName: "payments-eu",
          connectionUid: "4e3d2c1b-0a9f-4e8d-bc7b-6a5f4e3d2c1b",
          fingerprint: "sha256:7f3a91c20bd4e86f00000000000000000000000000000000000000000000abcd",
          agentConnected: true,
          namespaces: ["payments", "ledger"],
          accounts: [{ id: "alex", roles: ["viewer", "approver"] }],
          completion: {
            targetServer: "https://api.payments-eu.example.com:6443",
            targetCaSha256: "sha256:1111111111111111111111",
          },
        },
        {
          clusterName: "staging",
          connectionUid: "5f4e3d2c-1b0a-4f9e-8dcb-7a6b5f4e3d2c",
          fingerprint: "sha256:00000000000000000000000000000000000000000000000000000000000000ff",
          expiresAt: minutesAgo(-5),
        },
      ]
    })
    renderConsole("/connections", hosted(fixture))

    expect(await screen.findByText(/Waiting for the helper on the target/)).toBeInTheDocument()
    expect(screen.getByText("alex: viewer, approver")).toBeInTheDocument()
    expect(screen.getByText("payments, ledger")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Activate staging" })).toBeNull()
    await user.click(await screen.findByRole("button", { name: "Activate payments-eu" }))

    await waitFor(() =>
      expect(fixture.calls).toEqual([
        {
          method: "activateEnrollment",
          request: expect.objectContaining({ clusterName: "payments-eu" }),
        },
      ])
    )
  })
})
