import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect"
import { QueryClient } from "@tanstack/react-query"
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { AppProviders } from "./AppProviders"
import { BuildFooter } from "./BuildFooter"
import { InfoService, Mode } from "./gen/ptah/console/v1/info_pb"

function renderWith(getInfo: Parameters<typeof createRouterTransport>[0]) {
  const transport = createRouterTransport(getInfo)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <AppProviders transport={transport} queryClient={queryClient}>
      <BuildFooter />
    </AppProviders>
  )
}

describe("BuildFooter", () => {
  it("shows the version the server reports", async () => {
    renderWith(({ service }) => {
      service(InfoService, {
        getInfo: () => ({ version: "v0.1.0", commit: "abc", mode: Mode.LOCAL, readOnly: false }),
      })
    })

    expect(await screen.findByText("ptah-console v0.1.0")).toBeInTheDocument()
  })

  it("says when the server refuses every mutation", async () => {
    renderWith(({ service }) => {
      service(InfoService, {
        getInfo: () => ({ version: "v0.1.0", commit: "abc", mode: Mode.LOCAL, readOnly: true }),
      })
    })

    expect(await screen.findByText(/read-only/)).toBeInTheDocument()
  })

  it("says when the API cannot be reached", async () => {
    renderWith(({ service }) => {
      service(InfoService, {
        getInfo: () => {
          throw new ConnectError("down", Code.Unavailable)
        },
      })
    })

    expect(await screen.findByText("The console API is not reachable.")).toBeInTheDocument()
  })
})
