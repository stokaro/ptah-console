# Ptah Console

An optional web console for [Ptah Operator](https://github.com/stokaro/ptah-operator).

It answers the questions a person has when a schema change does not reach the
database on its own: what is happening, why, what is waiting for me, and which
action is valid next. A person can review the exact SQL of a plan and approve
that plan under their own Kubernetes identity; the operator's admission and
reconciliation stay the authority.

The console is not required to run the operator, and removing it does not
affect anything the operator does.

## Status

Under construction; there is no release yet. The plan and its steps are in
[stokaro/ptah-operator#587](https://github.com/stokaro/ptah-operator/issues/587),
and the architecture and security contract is
[stokaro/ptah-operator#588](https://github.com/stokaro/ptah-operator/issues/588).
[docs/design](docs/design/README.md) says what each screen reads, how the
console authenticates, and which operator versions it works with.

## Running it on your workstation

```bash
make build
./bin/ptah-console local --context <context> --namespace <namespace>
```

`local` acts with your own kubeconfig: every read and every decision goes to
the API server as you, so RBAC and the operator's admission judge you and
nobody else. It pins the context at start, listens on loopback only, and opens
the browser with a sign-in link that works once, for one minute; press Enter
in the terminal for a new one. `--no-open` prints the link instead,
`--read-only` refuses every change on the server, and audit lines go to
stderr.

The page has four views of the same resources:

- **Approver**: plans waiting for a decision. A plan shows what it changes and
  its exact SQL, read through the operator's integrity-checked plan reader and
  shown as plain text. Approving sends that plan, by UID and fingerprint, and
  nothing else; admission records who approved.
- **Developer**: each schema and migration as steps, with one sentence on
  where it stands and what happened.
- **On call**: what needs a person, the next step the operator supports, and
  the evidence Kubernetes still keeps: the Jobs the status names, their Pods,
  events and the end of their logs. A migration run the operator could not
  settle is acknowledged here.
- **Auditor**: who decided what and when, as admission recorded it, with a CSV
  export.

The page follows changes live. What a button offers depends on what RBAC says
you may do; the server decides.

A hosted mode, with personal Console accounts mapped to exact Kubernetes
identities through impersonation, follows.

## Demo

The Demo workflow records the basic scenario against the operator's own
demonstration lab: an approver signs in, reviews the plan, approves it, and
follows it until the operator has applied it and verified it. Its
`ptah-console-demo` artifact holds the video.

## Building

```bash
make build                       # the binary with the web app
make test                        # Go unit tests
make test-envtest                # the API against a real API server
cd web && npm test               # the web app
cd web && npm run dev --workspace app   # then open /fixture.html: the screens on test data
```

## License

[MIT](LICENSE).
