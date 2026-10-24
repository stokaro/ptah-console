import { create } from "@bufbuild/protobuf"
import { describe, expect, it } from "vitest"

import { Mode } from "../gen/ptah/console/v1/info_pb"
import { Access, NamespaceScopeSchema } from "../gen/ptah/console/v1/resources_pb"
import {
  kubectlCommand,
  kubernetesCheck,
  liveCheck,
  minorOf,
  namespacesCheck,
  scheduleCheck,
} from "./preflight"

describe("A Kubernetes version's minor", () => {
  it.each([
    ["v1.36.4", "1.36"],
    ["v1.36.4+k3s1", "1.36"],
    ["v1.37.0-eks-a1b2c3", "1.37"],
    ["1.35.2", "1.35"],
  ])("of %s is %s", (version, minor) => {
    expect(minorOf(version)).toBe(minor)
  })

  it.each(["", "unknown", "v1"])("of %j is not known", (version) => {
    expect(minorOf(version)).toBeUndefined()
  })
})

describe("The Kubernetes check", () => {
  const tested = ["1.35", "1.36", "1.37"]

  it("passes a tested minor, whatever its patch or vendor suffix", () => {
    expect(kubernetesCheck("v1.35.9+k3s1", tested).tone).toBe("ok")
  })

  it("warns about a minor outside the tested ones, newer or older", () => {
    expect(kubernetesCheck("v1.38.0", tested).tone).toBe("warn")
    expect(kubernetesCheck("v1.34.7", tested).tone).toBe("warn")
  })

  it("judges nothing it cannot read, or with nothing recorded to judge by", () => {
    expect(kubernetesCheck("", tested).tone).toBe("unk")
    expect(kubernetesCheck("v1.36.4", []).tone).toBe("unk")
  })
})

describe("The namespaces check", () => {
  const scope = (name: string, access: Access) => create(NamespaceScopeSchema, { name, access })

  it("passes when every namespace offered is readable and the list is whole", () => {
    expect(namespacesCheck([scope("a", Access.OK)], false, 64)).toEqual({
      tone: "ok",
      title: "Namespaces",
      detail: "May read 1 of the 1 offered.",
    })
  })

  it("fails when none is offered or none is readable", () => {
    expect(namespacesCheck([], false, 64).tone).toBe("bad")
    expect(namespacesCheck([scope("a", Access.FORBIDDEN)], false, 64).tone).toBe("bad")
  })

  it("warns when one is unreadable, or the list was cut", () => {
    expect(namespacesCheck([scope("a", Access.OK), scope("b", Access.ERROR)], false, 64).tone).toBe(
      "warn"
    )
    expect(namespacesCheck([scope("a", Access.OK)], true, 64).detail).toContain(
      "the console offers the first 64"
    )
  })
})

describe("The live check", () => {
  it.each([
    ["live", "ok"],
    ["connecting", "run"],
    ["lost", "warn"],
    ["denied", "warn"],
    ["ended", "bad"],
  ] as const)("reads %s as %s", (status, tone) => {
    expect(liveCheck({ status }, true).tone).toBe(tone)
  })

  it("judges nothing when there is nothing to follow", () => {
    expect(liveCheck({ status: "live" }, false).tone).toBe("off")
  })
})

describe("The schedule check", () => {
  it("judges nothing without a promise, warns when one is late and passes otherwise", () => {
    expect(scheduleCheck({ judged: 0, late: [] }).tone).toBe("off")
    expect(scheduleCheck({ judged: 2, late: [] }).detail).toBe(
      "2 resources were read when promised."
    )
    expect(scheduleCheck({ judged: 1, late: [] }).detail).toBe("1 resource was read when promised.")
  })
})

describe("The kubectl command", () => {
  it("asks through the kubeconfig context a local console uses", () => {
    expect(
      kubectlCommand({ mode: Mode.LOCAL, context: "kind-ptah", kubernetesUsername: "alice" }, [
        "application",
      ])
    ).toBe(
      'for ns in application; do echo "== $ns"; kubectl --context kind-ptah auth can-i --list --namespace "$ns"; done'
    )
  })

  it("asks as the account a hosted console impersonates", () => {
    expect(
      kubectlCommand(
        { mode: Mode.HOSTED, context: "in-cluster", kubernetesUsername: "ptah-console:prod:alex" },
        ["a", "b"]
      )
    ).toContain("for ns in a b; do")
  })

  it("quotes a context a shell would split or expand", () => {
    expect(
      kubectlCommand(
        { mode: Mode.LOCAL, context: "prod $(rm -rf ~)'s", kubernetesUsername: "alice" },
        ["a"]
      )
    ).toContain(`kubectl --context 'prod $(rm -rf ~)'"'"'s' auth`)
  })

  it("names a placeholder when no namespace is shown", () => {
    expect(kubectlCommand(undefined, [])).toContain("for ns in <namespace>; do")
  })
})
