import { screen } from "@testing-library/react"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import { renderConsole } from "../test/console"

const provider = {
  login: () => {
    throw new Error("an OIDC console takes no password")
  },
  provider: "Example SSO",
}

describe("Signing in with an OpenID Connect provider", () => {
  it("offers the provider, and no password form", async () => {
    const { container, history } = renderConsole("/approve", { hosted: provider })

    const link = await screen.findByRole("link", { name: "Sign in with Example SSO" })
    expect(link).toHaveAttribute("href", "/auth/oidc/login")
    expect(history.location.pathname).toBe("/login")
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
    expect(await axe(container)).toHaveNoViolations()
  })

  it.each([
    ["refused", "The API server does not accept Example SSO's token for this console."],
    ["state", "That sign-in was started in another window, or it took longer than ten minutes."],
    ["provider", "Example SSO did not sign you in."],
    ["unavailable", "The API server did not answer, so the console could not learn who you are."],
  ])("says why the sign-in came back without a session (%s)", async (reason, says) => {
    renderConsole(`/login?error=${reason}`, { hosted: provider })

    expect(await screen.findByRole("alert")).toHaveTextContent(says)
  })

  it("names no reason the console does not give", async () => {
    renderConsole("/login?error=<script>", { hosted: provider })

    expect(await screen.findByRole("link", { name: "Sign in with Example SSO" })).toBeVisible()
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })

  it("shows a signed-in person as signed in with OIDC", async () => {
    renderConsole("/approve", {
      hosted: { ...provider, signedIn: true },
      username: "oidc:alex@example.com",
      schemas: [],
      migrations: [],
    })

    expect(await screen.findByText("oidc:alex@example.com")).toBeVisible()
    expect(screen.getByText(/· OIDC in-cluster/)).toBeVisible()
  })

  it("names the session's source as OIDC, never as a Console account", async () => {
    renderConsole("/console", {
      hosted: { ...provider, signedIn: true },
      username: "oidc:alex@example.com",
    })

    const identity = await screen.findByRole("region", { name: "Acting as" })
    expect(identity).toHaveTextContent("oidc:alex@example.com")
    expect(identity).toHaveTextContent("OIDC · in-cluster")
    expect(identity).not.toHaveTextContent("Console account")
  })
})
