/**
 * The reference a change of source takes: an OCI reference pinned by a
 * SHA-256 digest, read by the rules kubectl ptah set-source applies before it
 * reads the cluster.
 *
 * This is the browser's copy of internal/ociref, which is the operator's own
 * parser over oras-go's registry.ParseReference. It exists for inline
 * validation only: the server parses the reference again, and the API server
 * and the operator's admission decide. Both copies are held to the
 * operator's table of accepted and refused values.
 */

const scheme = "oci://"

/** The shape a pinned reference takes, as a person writes it. */
export const pinnedForm = "oci://HOST/REPOSITORY@sha256:<64 hex>"

export type ParsedReference =
  | { ok: true; reference: string; digest: string }
  | {
      ok: false
      value: string
      /**
       * The tag the value names in place of a digest, when it names one: a
       * value `ptah oci resolve` can turn into a pinned reference.
       */
      tag: string
      problem: string
      /** The refusal as the operator words it: the value, then the problem. */
      message: string
    }

const sha256Digest = /^sha256:[0-9a-f]{64}$/
// A tag as oras-go validates one where no digest follows.
const tagName = /^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$/
// oras-go's repositoryRegexp reads the same language, with "[-]*" between
// alphanumeric runs. Zero hyphens there joins two runs into one, so each run
// can be split many ways, which RE2 in Go reads in one pass and a
// backtracking engine such as this one may not finish. Here a separator is
// one or more hyphens, so every run has exactly one reading.
const repositoryComponent = "[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*"
const repositoryPattern = new RegExp(`^${repositoryComponent}(?:/${repositoryComponent})*$`)
// The digests go-digest validates, with the hex length of each.
const digestLengths: Record<string, number> = { sha256: 64, sha384: 96, sha512: 128 }
// The characters Go's net/url keeps in a host as they are: unreserved, the
// sub-delimiters, a few it tolerates, and anything outside ASCII. Anything
// else, a "%" escape included, makes the parsed host differ from what was
// written.
const hostCharacter = /^[A-Za-z0-9\-._~!$&'()*+,;=<>"\]:\u{80}-\u{10FFFF}]$/u
// go-digest's pattern of a digest whose algorithm it does not know.
const anyDigest = /^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$/
// The white space Go's strings.TrimSpace removes: unicode.IsSpace.
const goSpace = /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]$/u
// What strconv.Quote prints as it is, beside printable ASCII: unicode.IsPrint.
const goPrintable = /^[\p{L}\p{M}\p{N}\p{P}\p{S}]$/u

function refuse(value: string, problem: string, tag = ""): ParsedReference {
  return { ok: false, value, tag, problem, message: `${goQuote(value)} ${problem}` }
}

/** A value quoted the way Go's %q quotes it, so a refusal reads as the server's. */
export function goQuote(value: string) {
  const escapes: Record<string, string> = {
    "\x07": "\\a",
    "\b": "\\b",
    "\f": "\\f",
    "\n": "\\n",
    "\r": "\\r",
    "\t": "\\t",
    "\v": "\\v",
    '"': '\\"',
    "\\": "\\\\",
  }
  let out = '"'
  for (const character of value) {
    const point = character.codePointAt(0) ?? 0
    if (escapes[character]) {
      out += escapes[character]
    } else if ((point >= 0x20 && point < 0x7f) || (point >= 0x80 && goPrintable.test(character))) {
      out += character
    } else if (point < 0x80) {
      out += `\\x${point.toString(16).padStart(2, "0")}`
    } else if (point < 0x10000) {
      out += `\\u${point.toString(16).padStart(4, "0")}`
    } else {
      out += `\\U${point.toString(16).padStart(8, "0")}`
    }
  }
  return `${out}"`
}

/**
 * Reads a reference that names its artifact by digest alone. A tag is refused
 * even beside a digest: the operator's parser drops such a tag unread. What
 * sits between the first ":" and the "@" is named for what it is, a tag or a
 * repository path that is not one, such as another reference pasted after
 * the registry.
 */
export function parsePinned(value: string): ParsedReference {
  const path = repositoryPath(value)
  const at = path.indexOf("@")
  const written = at === -1 ? path : path.slice(0, at)
  const digest = at === -1 ? "" : path.slice(at + 1)
  const parsed = parse(value)
  if (written.includes("://")) {
    let problem = `has ${written} where its repository path belongs, and a repository path carries no scheme`
    if (parsed.ok) {
      problem += `: the operator would read repository ${parsed.repository} from it and drop the rest unread`
    }
    return refuse(value, problem)
  }
  if (!parsed.ok) {
    return refuse(value, `is not an OCI reference the operator accepts: ${parsed.error}`)
  }
  const colon = written.indexOf(":")
  const tag = colon === -1 ? "" : written.slice(colon + 1)
  if (at === -1 && colon === -1) {
    return refuse(value, "names no digest, and no tag either, which means tag latest", "latest")
  }
  if (at === -1) {
    // The parser accepted it, so it is a tag.
    return refuse(value, `names tag ${tag}, not a digest`, tag)
  }
  if (colon !== -1 && !tagName.test(tag)) {
    return refuse(
      value,
      `has ${written} where its repository path belongs, which is not a repository path, with a tag or without: the operator would read repository ${parsed.repository} from it and drop the rest unread`
    )
  }
  if (colon !== -1) {
    return refuse(
      value,
      `names tag ${tag} beside its digest; the operator reads the digest alone, so the tag would be stored and never checked`
    )
  }
  if (!sha256Digest.test(digest)) {
    return refuse(
      value,
      `names digest ${digest}; the operator pins an artifact by sha256: and 64 lowercase hexadecimal digits`
    )
  }
  return { ok: true, reference: value, digest }
}

/** What follows the registry: the repository and its selector. */
function repositoryPath(value: string) {
  const rest = value.startsWith(scheme) ? value.slice(scheme.length) : value
  const slash = rest.indexOf("/")
  return slash === -1 ? "" : rest.slice(slash + 1)
}

type Parse = { ok: true; repository: string } | { ok: false; error: string }

/** The operator's own reading of a reference, then oras-go's parser. */
function parse(raw: string): Parse {
  const characters = [...raw]
  if (
    raw === "" ||
    goSpace.test(characters[0]) ||
    goSpace.test(characters[characters.length - 1]) ||
    /[ \t\r\n]/.test(raw)
  ) {
    return { ok: false, error: "OCI reference must not be empty or contain whitespace" }
  }
  if (!raw.startsWith(scheme)) {
    return { ok: false, error: "OCI reference must use the oci:// scheme" }
  }
  const value = raw.slice(scheme.length)
  const lower = value.toLowerCase()
  if (/[?#\\\0]/.test(value)) {
    return { ok: false, error: "OCI reference contains unsupported query, fragment, or path data" }
  }
  if (lower.includes("%2f") || lower.includes("%5c")) {
    return { ok: false, error: "OCI reference contains an escaped path separator" }
  }
  const firstSlash = value.indexOf("/")
  const firstAt = value.indexOf("@")
  if (firstAt >= 0 && (firstSlash < 0 || firstAt < firstSlash)) {
    return { ok: false, error: "OCI reference must not contain embedded credentials" }
  }
  return parseReference(value)
}

/** oras-go's registry.ParseReference, the four forms it accepts. */
function parseReference(artifact: string): Parse {
  const invalid = (what: string) => ({
    ok: false as const,
    error: `parse OCI reference: invalid reference: ${what}`,
  })
  const slash = artifact.indexOf("/")
  if (slash === -1) {
    return invalid("missing registry or repository")
  }
  const registry = artifact.slice(0, slash)
  const path = artifact.slice(slash + 1)
  let repository = path
  let reference = ""
  let isTag = false
  const at = path.indexOf("@")
  if (at !== -1) {
    repository = path.slice(0, at)
    reference = path.slice(at + 1)
    const colon = repository.indexOf(":")
    if (colon !== -1) {
      // A tag beside a digest is dropped without validation.
      repository = repository.slice(0, colon)
    }
  } else if (path.includes(":")) {
    const colon = path.indexOf(":")
    isTag = true
    repository = path.slice(0, colon)
    reference = path.slice(colon + 1)
  }
  if (!validRegistry(registry)) {
    return invalid(`invalid registry ${goQuote(registry)}`)
  }
  if (!repositoryPattern.test(repository)) {
    return invalid(`invalid repository ${goQuote(repository)}`)
  }
  if (reference === "") {
    return { ok: true, repository }
  }
  if (isTag) {
    return tagName.test(reference)
      ? { ok: true, repository }
      : invalid(`invalid tag ${goQuote(reference)}`)
  }
  const problem = digestProblem(reference)
  return problem === ""
    ? { ok: true, repository }
    : invalid(`invalid digest ${goQuote(reference)}: ${problem}`)
}

/** What go-digest's Validate says of a digest, or nothing when it holds. */
function digestProblem(digest: string) {
  const format = "invalid checksum digest format"
  const colon = digest.indexOf(":")
  if (colon <= 0 || colon + 1 === digest.length) {
    return format
  }
  const encoded = digest.slice(colon + 1)
  const algorithm = digest.slice(0, colon)
  const length = Object.hasOwn(digestLengths, algorithm) ? digestLengths[algorithm] : undefined
  if (length === undefined) {
    return anyDigest.test(digest) ? "unsupported digest algorithm" : format
  }
  // Go measures the encoded part in bytes.
  if (new TextEncoder().encode(encoded).length !== length) {
    return "invalid checksum digest length"
  }
  return new RegExp(`^[a-f0-9]{${length}}$`).test(encoded) ? "" : format
}

/**
 * Whether Go's url.ParseRequestURI reads the registry back as the host it
 * was written as, which is how oras-go validates it.
 */
function validRegistry(registry: string) {
  if (registry === "") {
    return false
  }
  const open = registry.lastIndexOf("[")
  if (open > 0) {
    return false
  }
  if (open === 0) {
    const close = registry.lastIndexOf("]")
    return (
      close > 0 && validOptionalPort(registry.slice(close + 1)) && isIPv6(registry.slice(1, close))
    )
  }
  if (registry.includes(":") && !validOptionalPort(registry.slice(registry.lastIndexOf(":")))) {
    return false
  }
  return [...registry].every((character) => hostCharacter.test(character))
}

function validOptionalPort(port: string) {
  return port === "" || /^:[0-9]*$/.test(port)
}

/** An IPv6 address, with an IPv4 tail allowed, and no zone. */
function isIPv6(text: string) {
  const halves = text.split("::")
  if (halves.length > 2) {
    return false
  }
  const groups = (half: string) => (half === "" ? [] : half.split(":"))
  const parts = halves.flatMap(groups)
  const last = parts.at(-1) ?? ""
  const ipv4 = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/.test(
    last
  )
  const hex = ipv4 ? parts.slice(0, -1) : parts
  const width = hex.length + (ipv4 ? 2 : 0)
  if (!hex.every((group) => /^[0-9A-Fa-f]{1,4}$/.test(group))) {
    return false
  }
  return halves.length === 2 ? width < 8 : width === 8
}
