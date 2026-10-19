# AGENTS.md

Repository-local guidance for coding agents working in `ptah-console`. It is
equally the rulebook for people.

## What this is

An optional web console for [Ptah Operator](https://github.com/stokaro/ptah-operator).
It shows what the operator is doing with `PtahSchema` and `PtahMigration`
resources, why something is blocked, and which action is valid next, and it
lets a person approve the exact plan they reviewed under their own Kubernetes
identity.

The work is tracked in stokaro/ptah-operator#587. The architecture and
security contract is stokaro/ptah-operator#588; read it before changing how
the console authenticates, reaches the API server, or acts.

One Go binary, `ptah-console`, serves the API and the embedded web app. It
runs in two modes: `local`, against the user's own kubeconfig on a loopback
listener, and `hosted`, beside the operator with personal Console accounts
and exact Kubernetes impersonation.

Read versions out of `go.mod` and `web/package.json` rather than out of prose
here.

## Boundaries

These hold in every mode and every change. A change that needs to cross one is
a change to #588 first.

- The console is a typed Kubernetes client. It is not a reconciler, a SQL
  executor, a generic Kubernetes proxy or a certificate authority. The
  operator and the API server decide; the console asks and reports.
- Every user request runs through a client bound to that user's identity:
  their kubeconfig in local mode, an impersonated personal identity in hosted
  mode. There is no client without an identity for user operations, and no
  fallback to the console's own ServiceAccount, another context, or another
  user.
- The identity comes only from the verified session and the server's own
  configuration. `Impersonate-*`, `X-Remote-*` and actor fields from a request
  are ignored.
- The console never reads Secrets, `PtahResultRecord` objects or arbitrary
  ConfigMaps, and never writes status, Leases, workloads or RBAC. Plan SQL is
  read only through the operator's `pkg/planread`, which applies the store's
  own integrity checks; nothing renders a partial or unverified plan.
- An approval names the exact owner and plan UIDs and the fingerprint the
  person reviewed. The console re-reads before sending and never approves
  "whatever is current". Admission stamps who approved.
- A capability check (`SelfSubjectAccessReview`) is a hint for the UI. The real
  request decides. Unknown is shown as unavailable.
- Cluster data is untrusted text: SQL, annotations, Events and logs render as
  text, never as HTML or terminal control sequences.
- No retry, rollback, force or cancel action exists unless the operator has a
  supported semantic for it.

## Building and testing

```bash
make build          # the ptah-console binary, into bin/
make test           # unit tests, with the race detector
make check          # gofmt, go mod tidy, vet, build, test: the Verify job
make lint           # golangci-lint, qtlint, nolintguard, teststyle
make vulncheck      # govulncheck
make verify         # everything CI runs
```

The Go lint setup is stokaro/ptah's: change `.golangci.yml` the way you would
there, and give every `//nolint` and `#nosec` a reason. Each later step adds
its targets here as it lands.

## What a proof owes

- **Assert the condition, never the phase it passes through.** A resource that
  waits still resolves and re-reads, so it leaves a phase for part of every
  cycle while the condition stays true.
- **A filter has to be shown to refuse something.** Every classifier, matcher
  and allowlist has a test with input it accepts and close input it must
  refuse. A test that passed over an empty collection proved nothing; count
  what was examined and fail on zero.
- **A fixed clock makes a comparison between two instants vacuous.** Under a
  fake clock, date the fixture's side distinctly before asserting one instant
  follows another.
- **A row that skips is not a row that passes.** Read what the suite actually
  skips with `go test -v`.
- **Show a new check failing once.** Break the code it guards and see it fail
  before the change is called done.

Tests use the standard `testing` package with quicktest imported as `qt`. Test
packages are black-box (`package x_test`) unless a test needs an unexported
detail.

## Language

American English in code, comments, documentation, issues, pull requests and
program output. Plain international English: short sentences, concrete nouns,
active voice. Say what changed and why, once.

Cut the preamble: a clause that labels a statement instead of making one gives
the reader nothing. Do not open a passage by counting what follows.

No AI attribution anywhere in git or on the forge: no co-author trailer, no
generated-with footer, no session or transcript reference.
