import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect"
import { createMemoryHistory } from "@tanstack/react-router"
import { QueryClient } from "@tanstack/react-query"
import { render, screen, within } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { App } from "./App"
import { AppProviders } from "./AppProviders"
import { InfoService, Mode } from "./gen/ptah/console/v1/info_pb"
import { SessionService } from "./gen/ptah/console/v1/session_pb"
import { renderConsole } from "./test/console"

describe("App", () => {
  it("says whom it acts as, offers the roles, and has no accessibility violations", async () => {
    const { container } = renderConsole("/", {
      username: "system:serviceaccount:application:demo-approver",
    })

    expect(
      await screen.findByText("system:serviceaccount:application:demo-approver")
    ).toBeInTheDocument()
    const roles = screen.getByRole("navigation", { name: "Role" })
    expect(
      within(roles)
        .getAllByRole("link")
        .map((link) => link.textContent)
    ).toEqual(["Approver", "Developer", "On call", "Auditor"])
    expect(within(roles).getByRole("link", { name: "Approver" })).toHaveAttribute(
      "aria-current",
      "page"
    )
    expect(await screen.findByText("ptah-console v0.1.0")).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it("asks for the terminal's link when the browser has no session", async () => {
    const transport = createRouterTransport(({ service }) => {
      service(InfoService, { getInfo: () => ({ version: "v0.1.0", mode: Mode.LOCAL }) })
      service(SessionService, {
        getSession: () => {
          throw new ConnectError("sign in first", Code.Unauthenticated)
        },
      })
    })
    render(
      <AppProviders
        transport={transport}
        queryClient={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <App history={createMemoryHistory({ initialEntries: ["/approve"] })} />
      </AppProviders>
    )

    expect(
      await screen.findByRole("heading", { name: "This page is not signed in" })
    ).toBeInTheDocument()
    expect(screen.queryByRole("navigation", { name: "Role" })).toBeNull()
  })
})
