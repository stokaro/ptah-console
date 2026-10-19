import { createRouterTransport } from "@connectrpc/connect"
import { render, screen } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { App } from "./App"
import { AppProviders } from "./AppProviders"
import { InfoService, Mode } from "./gen/ptah/console/v1/info_pb"

const transport = createRouterTransport(({ service }) => {
  service(InfoService, {
    getInfo: () => ({ version: "v0.1.0", commit: "abc", mode: Mode.LOCAL, readOnly: false }),
  })
})

describe("App", () => {
  it("names itself and has no accessibility violations", async () => {
    const { container } = render(
      <AppProviders transport={transport}>
        <App />
      </AppProviders>
    )

    expect(screen.getByRole("heading", { level: 1, name: "Ptah Console" })).toBeInTheDocument()
    expect(await screen.findByText("ptah-console v0.1.0")).toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })
})
