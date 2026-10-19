import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect"
import { describe, expect, it, vi } from "vitest"

import { SessionService } from "./gen/ptah/console/v1/session_pb"
import { exchangeLaunchToken } from "./launch"

function place(href: string) {
  const url = new URL(href)
  const location = { hash: url.hash, pathname: url.pathname, search: url.search } as Location
  const history = { replaceState: vi.fn() } as unknown as History
  return { location, history }
}

describe("exchangeLaunchToken", () => {
  it("removes the token from history before it is sent, and sends it once", async () => {
    const sent: string[] = []
    const { location, history } = place(
      "http://127.0.0.1:8443/approve?ns=application#t=abc_DEF-123"
    )
    const transport = createRouterTransport(({ service }) => {
      service(SessionService, {
        exchangeLaunchToken: (request) => {
          expect(history.replaceState).toHaveBeenCalledWith(null, "", "/approve?ns=application")
          sent.push(request.token)
          return {}
        },
      })
    })

    const exchanged = await exchangeLaunchToken(transport, location, history)

    expect(exchanged).toBe(true)
    expect(sent).toEqual(["abc_DEF-123"])
  })

  it("does nothing without a token", async () => {
    const { location, history } = place("http://127.0.0.1:8443/approve#section")
    const transport = createRouterTransport(() => {})

    const exchanged = await exchangeLaunchToken(transport, location, history)

    expect(exchanged).toBe(false)
    expect(history.replaceState).not.toHaveBeenCalled()
  })

  it("leaves no token behind when the server refuses it", async () => {
    const { location, history } = place("http://127.0.0.1:8443/#t=used")
    const transport = createRouterTransport(({ service }) => {
      service(SessionService, {
        exchangeLaunchToken: () => {
          throw new ConnectError("this link is not valid any more", Code.PermissionDenied)
        },
      })
    })

    await expect(exchangeLaunchToken(transport, location, history)).rejects.toThrow("not valid")
    expect(history.replaceState).toHaveBeenCalledWith(null, "", "/")
  })
})
