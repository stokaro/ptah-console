# Ptah Console

A web console for [Ptah Operator](https://github.com/stokaro/ptah-operator).

It answers the questions a person has when a schema change does not reach the
database on its own: what is happening, why, what is waiting for me, and which
action is valid next. A person can review the exact SQL of a plan and approve
that plan under their own Kubernetes identity; the operator's admission and
reconciliation stay the authority.

The console is not required to run the operator, and removing it does not
affect anything the operator does.

## Install

The console runs two ways:

- **On your workstation**, as `ptah-console local` with your own kubeconfig.
  Nothing is installed in the cluster. See [Run the console on your
  workstation](docs/install/local.md).
- **In the cluster**, from the chart, for people who sign in with a Console
  account that acts as its own Kubernetes username. See [Install a hosted
  console](docs/install/hosted.md) and [Running a hosted
  console](docs/hosted/runbook.md).

[Security model](docs/security.md) says whom each mode's requests run as and
what impersonation cannot contain. [Compatibility](docs/compatibility.md)
names the operator and Kubernetes versions, and [Releases](docs/releases.md)
how to verify what you download.

The plan this console follows is
[stokaro/ptah-operator#587](https://github.com/stokaro/ptah-operator/issues/587),
and its architecture and security contract is
[stokaro/ptah-operator#588](https://github.com/stokaro/ptah-operator/issues/588).
[docs/design](docs/design/README.md) says what each screen reads.

## What it shows

From a checkout, `make build` builds the binary with the web app, and
`./bin/ptah-console local --context <context> --namespace <namespace>` runs
it.

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

The **full console**, one link away, shows every resource at once: an
overview of what needs a person and how each namespace stands; a list that
filters, sorts and pages, with the filters in the address; a page for each
schema and migration with its conditions, plans, artifact, target, history
and spec; the execution and history of each, on controller, Job and database
lanes; and a pre-flight of what your identity may do in each namespace.
⌘K, Ctrl+K or `/` opens a palette that jumps to any page or resource.

The page follows changes live. What a button offers depends on what RBAC says
you may do; the server decides. [Using the console](docs/use/console.md) goes
through the tasks: approving a plan, following a release, finding out why
something failed, settling a run nobody can prove, creating a resource,
asking for a read, suspending, and seeing what you may do.

## Demo

[![An approver reviews a plan that drops a column and grants SELECT, with what it changes and its exact SQL](docs/demo/poster.png)](docs/demo/ptah-console-demo.mp4)

[The recording](docs/demo/ptah-console-demo.mp4) (7 minutes 29 seconds) runs the scenarios of
[stokaro/ptah-operator#587](https://github.com/stokaro/ptah-operator/issues/587)
against a real operator, one person at a time, each signed in to a
`ptah-console local` of their own with a client certificate the cluster
issued:

- an outsider, a viewer and a SQL reviewer see what their RBAC lets them see,
  and are told what it does not, including a namespace they may not read;
- an author is not offered approval, and one sent anyway is forbidden;
- a lead who wrote the change is refused their own approval by the operator's
  four-eyes control;
- an approver approves a plan that drops a column and grants access, and
  follows it until the operator has applied it and verified it by a fresh read;
- a plan replaced while the approver decides is not approved, and the page
  says so;
- migrations 14 to 17 are approved without SQL the operator API does not carry;
- on call, a failed run shows the evidence Kubernetes still keeps and the end
  of its log;
- a MySQL run that stopped halfway is settled by an acknowledgment, not a
  retry;
- a run whose Job was deleted mid-Apply is unknown until acknowledged, and an
  API server that stops answering meanwhile is reported while the decision
  waits;
- the auditor view shows who decided what, as admission recorded it;
- an author creates a schema from a ConfigMap, checked by the API server
  before it is created, and a migration from the up and down files of another;
- an author asks inventory to read its database now, and the operator reads it
  before its interval would;
- an approver reviews both new plans in the full console, approves them as
  themselves, and follows each to the database, with the history the journal
  keeps and the revision row the run wrote;
- an author suspends catalog and resumes it, and the operator follows both;
- on call reads a failed run in Execution & diagnosis and finds it, with the
  operator's Events, in Activity.

The rows run against the operator's own demonstration lab, from the operator
commit `go.mod` requires; the recording cuts out the minutes the operator
spends applying, and `make e2e-verify` checks every decision against the
cluster rather than the page. This file was recorded on October 24, 2026,
against operator commit `7484e85a` on Kubernetes 1.37, with `make e2e-up`,
`E2E_ROWS="acceptance.spec.ts console.spec.ts" make e2e-rows` and
`ROWS="acceptance.spec.ts console.spec.ts" make e2e-film`. The Acceptance
workflow records the same film on each run of every row, as
`ptah-console-demo.mp4` in its `ptah-console-acceptance` artifact, beside
`ptah-console-full.mp4` with the hosted and two-cluster rows.

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
