# Design

What the console reads, how it authenticates, how it is deployed, and which
operator versions it works with. stokaro/ptah-operator#587 asks for this
before the screens are built; stokaro/ptah-operator#588 is the architecture
and security contract it implements.

- [What each screen reads](ui-api-mapping.md): resources, verbs, and the role
  that grants them.
- [Authentication](authentication.md): local and hosted mode, sessions, and
  the limit of impersonation.
- [Deployment and compatibility](deployment-and-compatibility.md): the binary,
  the chart, supported versions, and how an action is judged supported.
- [Where the design and the contract differ](design-departures.md).
- [External clusters](external-clusters.md): the phase 2 design, reviewed on
  2026-10-21 before any of it is built.

## The request path

```text
browser ── same origin, session cookie, Connect ──> ptah-console
                                                       │  client bound to the person:
                                                       │  kubeconfig (local) or
                                                       │  ServiceAccount + Impersonate-User (hosted)
                                                       v
                                        kube-apiserver ── RBAC, operator admission ──> Ptah resources
```

The browser holds an opaque session and never a Kubernetes credential. Every
request a person makes runs through a Kubernetes client bound to that person,
built once per identity and never changed. There is no client without an
identity for user requests, so there is nothing to fall back to.

The console keeps two kinds of state, both in memory: sessions, and the
ledger of decisions it sent and has not yet read back. Everything else is read
from Kubernetes when it is needed, as the person, and nothing is cached across
identities.

Live updates are a server stream. On the server, a watch runs as the person in
windows of at most 30 seconds, each a newly authorized request, so a revoked
grant ends the stream within one window.
