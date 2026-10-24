import { describe, expect, it } from "vitest"

import { Mode, SignIn } from "../gen/ptah/console/v1/info_pb"
import { sessionSource } from "./session"

describe("Where a session's identity comes from", () => {
  it.each([
    { mode: Mode.LOCAL, signIn: SignIn.LAUNCH_LINK, source: "kubeconfig" },
    { mode: Mode.HOSTED, signIn: SignIn.ACCOUNTS, source: "account" },
    { mode: Mode.HOSTED, signIn: SignIn.OIDC, source: "oidc" },
    { mode: Mode.HOSTED, signIn: undefined, source: "account" },
  ])("reads $source from mode $mode and sign-in $signIn", ({ mode, signIn, source }) => {
    expect(sessionSource({ mode, signIn })).toBe(source)
  })
})
