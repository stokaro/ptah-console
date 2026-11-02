import { describe, expect, it } from "vitest"

import { parsePinned } from "./sourceref"
import corpus from "./testdata/pinned-references.json?raw"

// The operator's own rows: TestParsePinnedReference in
// internal/operate/source_test.go of stokaro/ptah-operator at v0.2.9, the
// table kubectl ptah set-source is held to. internal/ociref/ociref_test.go
// holds the server's copy of these rules to the same rows.
const digestNew = `sha256:${"b".repeat(64)}`
const pinnedNew = `oci://registry.example/shop/storefront@${digestNew}`

describe("parsePinned accepts the operator's pinned references", () => {
  it.each([
    ["a digest", pinnedNew],
    ["a registry with a port", `oci://localhost:5000/shop/storefront@${digestNew}`],
    ["a nested repository", `oci://registry.example/acme/shop/storefront@${digestNew}`],
  ])("%s", (_name, value) => {
    expect(parsePinned(value)).toEqual({ ok: true, reference: value, digest: digestNew })
  })
})

interface Refused {
  name: string
  value: string
  want: string
  tag?: string
  /** What the refusal must not call the value. */
  notWant?: string
}

const refused: Refused[] = [
  {
    name: "a tag",
    value: "oci://registry.example/shop/storefront:1.5.0",
    want: "names tag 1.5.0, not a digest",
    tag: "1.5.0",
  },
  {
    name: "a tag on a registry with a port",
    value: "oci://localhost:5000/shop/storefront:1.5.0",
    want: "names tag 1.5.0, not a digest",
    tag: "1.5.0",
  },
  {
    name: "no selector",
    value: "oci://registry.example/shop/storefront",
    want: "which means tag latest",
    tag: "latest",
  },
  {
    name: "a tag beside the digest",
    value: `oci://registry.example/shop/storefront:1.5.0@${digestNew}`,
    want: "names tag 1.5.0 beside its digest",
  },
  {
    name: "another reference after the registry",
    value: `oci://registry.example/oci://other.example/shop/storefront@${digestNew}`,
    want:
      "has oci://other.example/shop/storefront where its repository path belongs, and a repository path carries " +
      "no scheme: the operator would read repository oci from it and drop the rest unread",
    notWant: "tag",
  },
  {
    name: "another scheme after the registry",
    value: `oci://registry.example/https://other.example/shop/storefront@${digestNew}`,
    want: "has https://other.example/shop/storefront where its repository path belongs, and a repository path carries no scheme",
    notWant: "tag",
  },
  {
    name: "another reference after the registry, with no digest",
    value: "oci://registry.example/oci://other.example/shop/storefront:1.5.0",
    want: "has oci://other.example/shop/storefront:1.5.0 where its repository path belongs, and a repository path carries no scheme",
    notWant: 'tag "',
  },
  {
    name: "a path with a colon in it before the digest",
    value: `oci://registry.example/shop:storefront/v1@${digestNew}`,
    want:
      "has shop:storefront/v1 where its repository path belongs, which is not a repository path, with a tag or " +
      "without: the operator would read repository shop from it and drop the rest unread",
    notWant: "names tag",
  },
  {
    name: "an empty tag beside the digest",
    value: `oci://registry.example/shop/storefront:@${digestNew}`,
    want: "has shop/storefront: where its repository path belongs, which is not a repository path",
    notWant: "names tag",
  },
  {
    name: "two tags beside the digest",
    value: `oci://registry.example/shop/storefront:1.5:0@${digestNew}`,
    want: "which is not a repository path",
    notWant: "names tag",
  },
  {
    name: "a tag longer than a tag can be beside the digest",
    value: `oci://registry.example/shop/storefront:${"v".repeat(129)}@${digestNew}`,
    want: "which is not a repository path",
    notWant: "names tag",
  },
  {
    name: "the longest tag beside the digest",
    value: `oci://registry.example/shop/storefront:${"v".repeat(128)}@${digestNew}`,
    want: `names tag ${"v".repeat(128)} beside its digest`,
  },
  {
    name: "another algorithm",
    value: `oci://registry.example/shop/storefront@sha512:${"b".repeat(128)}`,
    want: "names digest sha512:",
  },
  {
    name: "upper-case hex",
    value: `oci://registry.example/shop/storefront@sha256:${"B".repeat(64)}`,
    want: "is not an OCI reference the operator accepts",
  },
  {
    name: "a short digest",
    value: `oci://registry.example/shop/storefront@sha256:${"b".repeat(63)}`,
    want: "is not an OCI reference the operator accepts",
  },
  {
    name: "no scheme",
    value: `registry.example/shop/storefront@${digestNew}`,
    want: "oci:// scheme",
  },
  {
    name: "a credential",
    value: `oci://user:secret@registry.example/shop/storefront@${digestNew}`,
    want: "embedded credentials",
  },
  {
    name: "a query",
    value: `${pinnedNew}?x=1`,
    want: "is not an OCI reference the operator accepts",
  },
  { name: "space around it", value: ` ${pinnedNew}`, want: "whitespace" },
]

describe("parsePinned refuses what the operator refuses", () => {
  it.each(refused)("$name", ({ value, want, tag = "", notWant }) => {
    const parsed = parsePinned(value)
    expect(parsed.ok).toBe(false)
    expect(parsed).toMatchObject({ ok: false, value, tag })
    const message = parsed.ok ? "" : parsed.message
    expect(message).toContain(want)
    expect(message.startsWith(JSON.stringify(value))).toBe(true)
    expect(notWant !== undefined && message.includes(notWant)).toBe(false)
  })
})

// The console's own rows, on what oras-go reads as a registry and a
// repository. internal/ociref/ociref_test.go carries the same rows and holds
// the server's parser to them.
describe("parsePinned reads the registry and repository as oras-go does", () => {
  it.each([
    ["an IPv6 registry with a port", `oci://[::1]:5000/shop/storefront@${digestNew}`, ""],
    [
      "a registry of host names joined by colons",
      `oci://a:b:5000/shop/storefront@${digestNew}`,
      "",
    ],
    [
      "an IPv4 address in brackets",
      `oci://[127.0.0.1]/shop/storefront@${digestNew}`,
      "is not an OCI reference the operator accepts",
    ],
    [
      "a port that is not a number",
      `oci://registry.example:port/shop/storefront@${digestNew}`,
      "is not an OCI reference the operator accepts",
    ],
    [
      "an escape in the registry",
      `oci://registry%41.example/shop/storefront@${digestNew}`,
      "is not an OCI reference the operator accepts",
    ],
    [
      "no registry",
      `oci:///shop/storefront@${digestNew}`,
      "is not an OCI reference the operator accepts",
    ],
    [
      "an upper-case repository",
      `oci://registry.example/Shop/storefront@${digestNew}`,
      "is not an OCI reference the operator accepts",
    ],
    [
      "a repository that ends in a separator",
      `oci://registry.example/shop/@${digestNew}`,
      "is not an OCI reference the operator accepts",
    ],
    [
      "another SHA-2 digest",
      `oci://registry.example/shop/storefront@sha384:${"b".repeat(96)}`,
      "names digest sha384:",
    ],
    [
      "an algorithm go-digest does not know",
      `oci://registry.example/shop/storefront@md5:${"b".repeat(32)}`,
      "is not an OCI reference the operator accepts",
    ],
  ])("%s", (_name, value, want) => {
    const parsed = parsePinned(value)
    expect(parsed.ok).toBe(want === "")
    expect(parsed.ok ? "" : parsed.message).toContain(want)
  })
})

interface Verdict {
  value: string
  ok: boolean
  digest?: string
  tag?: string
  message?: string
}

// The server's parser wrote what it makes of each value in the corpus
// (internal/ociref/corpus_test.go): every value the tables read, and values
// where Go and a browser read text differently. The browser's copy says the
// same, message for message.
describe("parsePinned says what the server's parser says", () => {
  const verdicts = JSON.parse(corpus) as Verdict[]

  it("of a corpus that holds both verdicts", () => {
    expect(verdicts.length).toBeGreaterThan(60)
    expect(verdicts.filter((verdict) => verdict.ok).length).toBeGreaterThan(5)
    expect(verdicts.filter((verdict) => !verdict.ok).length).toBeGreaterThan(30)
  })

  it.each(verdicts.map((verdict) => [JSON.stringify(verdict.value), verdict] as const))(
    "of %s",
    (_, verdict) => {
      const parsed = parsePinned(verdict.value)
      expect(
        parsed.ok
          ? { value: verdict.value, ok: true, digest: parsed.digest }
          : {
              value: verdict.value,
              ok: false,
              ...(parsed.tag ? { tag: parsed.tag } : {}),
              message: parsed.message,
            }
      ).toEqual(verdict)
    }
  )
})

describe("parsePinned reads a long repository path in one pass", () => {
  // oras-go's pattern, run by a backtracking engine, takes exponential time on
  // these: each run of zeros can be split at every position.
  it.each([
    ["a run after a dot", `oci://registry.example/0.${"0".repeat(40)}!@${digestNew}`],
    ["runs between slashes", `oci://registry.example/0/${"00/".repeat(40)}!@${digestNew}`],
    ["a run after a component", `oci://registry.example/0/0.${"0".repeat(40)}!@${digestNew}`],
  ])("%s", (_name, value) => {
    const started = Date.now()
    const parsed = parsePinned(value)
    expect(parsed.ok ? "" : parsed.message).toContain("invalid repository")
    expect(Date.now() - started).toBeLessThan(1000)
  })
})
