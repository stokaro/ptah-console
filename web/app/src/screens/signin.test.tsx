import { Code, ConnectError } from "@connectrpc/connect"
import { screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { axe } from "jest-axe"
import { describe, expect, it } from "vitest"

import type { LoginRequest } from "../gen/ptah/console/v1/session_pb"
import { renderConsole, waitingSchema } from "../test/console"

/** A hosted console with one account, alex, whose password is alex-secret. */
function hostedConsole(refusal?: Code) {
  const attempts: { login: string; password: string }[] = []
  return {
    attempts,
    hosted: {
      login: (request: LoginRequest) => {
        attempts.push({ login: request.login, password: request.password })
        if (refusal !== undefined) {
          throw new ConnectError("refused", refusal)
        }
        if (request.login !== "alex@example.com" || request.password !== "alex-secret") {
          throw new ConnectError("the login or the password is wrong", Code.Unauthenticated)
        }
      },
    },
  }
}

async function signIn(login: string, password: string) {
  const form = await screen.findByRole("form", { name: "Sign in" })
  await userEvent.type(within(form).getByLabelText("Login"), login)
  await userEvent.type(within(form).getByLabelText("Password"), password)
  await userEvent.click(within(form).getByRole("button", { name: "Sign in" }))
  return form
}

describe("Signing in to a hosted console", () => {
  it("sends a browser without a session to the sign-in form", async () => {
    const { hosted } = hostedConsole()
    const { history, container } = renderConsole("/approve", { hosted })

    expect(await screen.findByRole("form", { name: "Sign in" })).toBeInTheDocument()
    expect(history.location.pathname).toBe("/login")
    expect(screen.getByLabelText("Password")).toHaveAttribute("type", "password")
    expect(screen.getByLabelText("Password")).toHaveAttribute("autocomplete", "current-password")
    expect(await axe(container)).toHaveNoViolations()
  })

  it("signs the account in with what was typed and opens the console as its identity", async () => {
    const { hosted, attempts } = hostedConsole()
    const { history } = renderConsole("/approve", { hosted, schemas: [waitingSchema()] })

    await signIn("alex@example.com", "alex-secret")

    expect(await screen.findByText("ptah-console:prod:alex")).toBeInTheDocument()
    expect(screen.getByText("Alex Morgan")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument()
    await waitFor(() => expect(history.location.pathname).toBe("/approve"))
    expect(attempts).toEqual([{ login: "alex@example.com", password: "alex-secret" }])
  })

  it("says a refusal without saying which half was wrong, and clears the password", async () => {
    const { hosted } = hostedConsole()
    renderConsole("/login", { hosted })

    const form = await signIn("alex@example.com", "guess")

    expect(await within(form).findByRole("alert")).toHaveTextContent(
      "The login or the password is wrong."
    )
    expect(within(form).getByLabelText("Password")).toHaveValue("")
    expect(within(form).getByLabelText("Login")).toHaveValue("alex@example.com")
  })

  it("asks to wait after too many attempts", async () => {
    const { hosted } = hostedConsole(Code.ResourceExhausted)
    renderConsole("/login", { hosted })

    const form = await signIn("alex@example.com", "alex-secret")

    expect(await within(form).findByRole("alert")).toHaveTextContent(
      "Too many sign-in attempts. Wait a minute and try again."
    )
  })

  it("signs out back to the sign-in form", async () => {
    const { hosted } = hostedConsole()
    const { history } = renderConsole("/login", { hosted })
    await signIn("alex@example.com", "alex-secret")
    await userEvent.click(await screen.findByRole("button", { name: "Sign out" }))

    expect(await screen.findByRole("form", { name: "Sign in" })).toBeInTheDocument()
    expect(history.location.pathname).toBe("/login")
    expect(screen.queryByText("ptah-console:prod:alex")).toBeNull()
  })
})

describe("A session the server forgot", () => {
  it("goes back to the sign-in form once, though the page still holds the old session", async () => {
    const { hosted } = hostedConsole()
    const server = { ended: false }
    const { history, queryClient } = renderConsole("/login", { hosted, server })
    await signIn("alex@example.com", "alex-secret")
    expect(await screen.findByText("ptah-console:prod:alex")).toBeInTheDocument()

    // A restart of the console: the cookie names a session nobody holds now.
    server.ended = true
    await queryClient.invalidateQueries()

    expect(await screen.findByRole("form", { name: "Sign in" })).toBeInTheDocument()
    expect(history.location.pathname).toBe("/login")
    expect(screen.queryByText("ptah-console:prod:alex")).toBeNull()
  })
})

describe("A local session the server forgot", () => {
  it("goes to the launch page once, though the page still holds the old session", async () => {
    const server = { ended: false }
    const { history, queryClient } = renderConsole("/releases", { server })
    expect(await screen.findByText("alice")).toBeInTheDocument()

    // The session's idle limit passed, or ptah-console local was restarted.
    server.ended = true
    await queryClient.invalidateQueries()

    expect(await screen.findByText("This page is not signed in")).toBeInTheDocument()
    expect(history.location.pathname).toBe("/launch")
    expect(screen.queryByText("alice")).toBeNull()
  })
})

describe("A local console", () => {
  it("has no password form and no sign-out", async () => {
    renderConsole("/login")

    expect(await screen.findByText("alice")).toBeInTheDocument()
    expect(screen.queryByRole("form", { name: "Sign in" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Sign out" })).toBeNull()
  })
})
