# Architecture

How the console is put together across the phases of
stokaro/ptah-operator#588: the modes, whom each request runs as, how it
reaches a cluster, where state lives, and what the operator holds whatever
the console does. Each section links to the page with the details. The page
describes console 0.3.2, built against operator v0.2.9. Everything under
[Not built](#not-built) is proposed in #588 and not built.

## Phases

| Phase in #588 | What it is | Shipped |
| --- | --- | --- |
| 1, local and co-located | `ptah-console local` with the person's own kubeconfig; `ptah-console serve` beside the operator, with Console accounts and exact impersonation | console 0.1.0, on operator v0.2.5 |
| 2, external clusters | an outbound agent in each other cluster, the connection registry, enrollment, credential rotation, disable, quarantine and removal | console 0.2.0, on operator v0.2.8 |
| 3, optional identity and connectivity | sign-in with OpenID Connect | console 0.2.0, with the person's own token and on the hosting cluster only: see [Where the build departs from #588](#where-the-build-departs-from-588) |
| | OIDC through agents, native per-target credentials, a transport-only connector | not built |
| 4, scale and integrations | more than one replica, shared sessions, federation, external audit | not built |

## The parts

- `ptah-console`: one binary with the web app embedded. `local` serves one
  person on a loopback address. `serve` is the hosted console. Its other
  commands run on an administrator's machine: `accounts hash`,
  `config validate`, `cluster enroll`, which enrolls a target cluster with
  its administrator's own kubeconfig, and `cluster unenroll`, which removes
  the credential `enroll` made there.
- `ptah-console-agent`: the program a hosted console reaches another cluster
  through. It is released with the console and carries its version.
- The charts `charts/ptah-console` and `charts/ptah-console-agent`, each
  installed in a namespace of its own and never in the operator's.
- The registry, `console.ptah.run/v1alpha1`: `ConsoleCluster`,
  `ConsoleClusterPreferences`, `ConsoleClusterEnrollment` and
  `ConsoleClusterRevocation`, in the hosted console's namespace. The chart
  ships the CRDs, and a console uses them only with `registry.enabled`.
- From the operator, the console takes its API types and `pkg/planread`, the
  plan reader with the store's integrity checks, as a Go module at one
  released version. It reaches the operator only through the API server,
  never through its webhooks, its result receiver or its Pods.

## Modes and identity profiles

```text
local
  browser ──loopback──> ptah-console local ──kubeconfig context──> kube-apiserver

hosted, the cluster the console runs in
  browser ──HTTPS──> ptah-console serve ──ServiceAccount + Impersonate-User──> kube-apiserver
                                         (with OIDC: the person's ID token instead)

hosted, another cluster
  browser ──HTTPS──> ptah-console serve <──TLS channel, opened by the agent── ptah-console-agent
                                                                                       │ ServiceAccount + Impersonate-User
                                                                                       v
                                                                             target kube-apiserver
```

| Profile | The person proves who they are with | A request to Kubernetes carries | Admission records | The cluster needs |
| --- | --- | --- | --- | --- |
| Kubeconfig, local | the single-use launch link the console prints or opens on their own machine | the context's own credential | the kubeconfig's user | nothing installed |
| Console account, hosted | a login and a password, checked against a bcrypt hash | the console's ServiceAccount token, impersonating `ptah-console:<installationID>:<account>` | that username | the chart's exact impersonation rule and RoleBindings; no API server change |
| Console account, through an agent | the same sign-in, at the hosted console | the agent's ServiceAccount token, impersonating the same username | that username | the agent chart's exact impersonation rule and RoleBindings; no inbound connection and no API server change |
| OIDC, hosted | the provider's sign-in, and an ID token the console checks | the person's ID token as a bearer token, with no impersonation | the username the API server maps from the token | an API server that trusts the issuer, with the console's client ID as an audience |

These hold for every profile:

- A client is bound to one identity when it is built and never changes. A
  person's request goes only through that person's client. There is no client
  without an identity for user requests, so nothing falls back to the
  console's ServiceAccount, another context or another person.
- The identity comes from the verified session and the server's own
  configuration. A request that carries `Impersonate-*` or `X-Remote-*`
  headers is refused.
- Where a profile impersonates, it names the username alone, with no groups,
  UID or extras, so the API server adds only `system:authenticated`. Whatever
  RBAC grants the username, and that group, is what the account may do.
- A `SelfSubjectAccessReview` decides what a page offers, and the request
  decides what happens. The registry has the one exception, in
  [Where the build departs from #588](#where-the-build-departs-from-588).

Which combinations exist:

- A hosted configuration signs people in with `accounts` or with `oidc`,
  never both.
- The registry, and with it every external cluster, needs `accounts`, because
  agents act as Console accounts. A console with `oidc` reaches only the
  cluster it runs in.
- `local` acts with one context and has no registry.
- One person under two profiles is two Kubernetes identities. The console
  never merges them, by email or by anything else.

[Authentication](authentication.md), the [security model](../security.md),
[Hosted accounts](../hosted/accounts.md) and
[Sign in with OpenID Connect](../hosted/oidc.md) have the details.

## Connectivity

- **Local.** The browser reaches a loopback listener only, over IPv4 and IPv6,
  and every request has to carry its exact Host and Origin. The console dials
  the API server the context names.
- **The cluster the console runs in.** The console reads the API server's
  address and CA from its Pod. People reach the console through its Service,
  the chart's Ingress or HTTPRoute, or one of their own. It serves HTTPS
  itself, or plain HTTP behind a proxy the installer declares terminates TLS.
  With the chart's NetworkPolicy on, the console reaches only the API
  server's addresses, and `networkPolicy.ingressFrom` names who may reach the
  console.
- **Another cluster.** The agent dials the console's agent listener, which
  has a port, a Service and a certificate of its own, issued by the console's
  own CA. The target cluster accepts no inbound connection. The channel is one
  Connect bidirectional stream over HTTP/2 and TLS 1.3. The agent trusts only
  the console's CA and the server name it was enrolled with. The console
  accepts the agent by its channel credential and keeps a SHA-256 verifier of
  it. The agent calls only its own cluster's API server, with the typed
  requests the console's pages make: it is not a proxy. The console never
  dials a target's API server.
- **OIDC.** The console also reaches the provider: it reads the discovery
  document at start, and calls the token endpoint at sign-in and at each
  renewal. The chart's NetworkPolicy has no rule for the provider.

A connection that is up grants nothing by itself. Every request on it still
runs as one person, and RBAC on the target decides.
[External clusters](external-clusters.md) and
[Serving a hosted console](../hosted/serve.md) have the details.

## Where state lives

| State | Where | After a restart |
| --- | --- | --- |
| Sessions, launch links, OIDC sign-ins in progress, ID and refresh tokens | the memory of the one process | gone: everyone signs in again |
| The ledger of decisions sent and not yet read back | memory | gone, and nothing is replayed: a decision's object name is derived from who decided what, so its outcome is read back from Kubernetes by that name |
| The hosted configuration: installation ID, public URL, namespace ceiling, accounts | a ConfigMap rendered from the chart's values | kept |
| Password hashes, TLS keys, the OIDC client secret | Secrets the chart mounts by name | kept |
| The impersonation allowlist and each account's RoleBindings | RBAC objects the chart renders | kept |
| Connections, preferences, enrollments, revocations, credential verifiers | the registry, in the console's namespace; status written only by the console's ServiceAccount | kept |
| An agent's configuration and account allowlist | ConfigMaps rendered from the agent chart's values | kept |
| An agent's channel credential | a Secret in the target cluster, mounted into the agent | kept |
| The credential plugins a person agreed to run, in local mode | one state file, mode 0600 in a 0700 directory | kept |
| Schemas, migrations, plans, approvals and their history | the operator's own resources | the operator's |

The console has no database and copies nothing the operator holds. It reads
no Secret, `PtahResultRecord`, ConfigMap, `PtahRealm` or Lease, and writes no
status of an operator kind; [What each screen reads](ui-api-mapping.md) lists
what it does read.

## What the operator holds whatever the console does

The console sends ordinary requests to the API server, so it can do nothing a
person could not do with `kubectl` and the same identity. In operator v0.2.9
these hold whether a console is installed, down or removed:

- Approval and acknowledgment admission stamps who decided from the
  authenticated request, and overwrites any stamp the request carried. A
  stamped decision is immutable. Approval admission binds names to UIDs and
  refuses a plan whose storage is incomplete, or a changed policy or target.
  Its webhooks fail closed: while they are unreachable, no approval is
  admitted.
- Only the manager's ServiceAccount writes the status of operator kinds. A
  ValidatingAdmissionPolicy refuses everyone else, a cluster administrator
  included.
- `approvals.requireDistinctApprover`, off by default, refuses an approval
  from the identity that last wrote the resource's spec, and one for a
  resource with no recorded writer. It compares usernames, so one person with
  two Console accounts, or with an account and a kubeconfig, counts as two.
- `applyPolicyGuard`, on by default, refuses a move of `spec.policy.apply` to
  `Always` by anyone outside `applyPolicyGuard.exemptGroups`. A Console
  account has no group but `system:authenticated`, so it is never exempt.
  With OIDC the groups are the ones the API server maps.
- Admitted approvals stay admitted and running operations go on when the
  console stops, is uninstalled, or loses a cluster.

What admission records is the profile's username, so choose it knowing where
it goes. From operator v0.2.10 a `PtahMigration` Apply also writes the
approver's username into the migrated database, in
`schema_migrations_log.actor`; see
[the approver's name in the target database](https://operator.ptah.run/v0.2.10/use/security/#approver-in-the-database).
The operator's [security model](https://operator.ptah.run/v0.2.9/use/security/)
and its [trust boundaries](https://operator.ptah.run/v0.2.9/use/security/#trust-boundaries)
have the details.

## What the console cannot contain

The console defends against people without a grant, other websites, stale or
replayed decisions, one person's request running as another, and cluster data
rendered as markup. It does not contain:

- **Its own compromise.** Whoever runs code in the console's Pod, or reads its
  ServiceAccount token, acts with the union of every account's rights in that
  cluster. With OIDC, they act with the tokens of the people signed in at that
  moment. Keep workload creation, exec and token requests in the console's
  namespace to the console's administrators.
- **A trusting agent.** A compromised console can ask an agent to act as any
  account on the agent's allowlist. A compromised agent can use those
  accounts' rights in its cluster. Removing the agent's impersonation binding
  on the target cuts it off, whatever the console says.
- **The people above it.** Cluster administrators, whoever administers the
  operator's release namespace, and the identity provider's administrators.
- **Who a person is.** An impersonated or OIDC identity proves which identity
  acted, not which human.
- **A rollback of its own records.** Restoring the console's namespace from a
  backup brings back credentials revoked or rotated since then, until they
  expire. [Backup and restore](../hosted/external-clusters.md#backup-and-restore)
  gives the procedure.

The [security model](../security.md) has the details.

## Where the build departs from #588

| #588 | Built | Recorded |
| --- | --- | --- |
| §15: Console OIDC maps the provider's issuer and subject to an administrator-approved Console account, which is then impersonated, and no API server needs OIDC setup. §4: adding OIDC keeps each person's existing mapping. | The console passes the person's ID token to the hosting API server, which has to trust the issuer, and impersonates nobody. A configuration has `accounts` or `oidc`, so a person's Kubernetes username changes with that choice, and an OIDC console has no registry and no external cluster. | Owner decision of 2026-10-24 in stokaro/ptah-console#123; [Sign in with OpenID Connect](../hosted/oidc.md) |
| §13: `ConsoleCluster` and `ConsoleClusterPreferences`, with enrollment and verifier state in installation ConfigMaps and Secrets. | Two more kinds, `ConsoleClusterEnrollment` and `ConsoleClusterRevocation`. The verifiers are in the status of the enrollment and of the `ConsoleCluster`, which only the console's ServiceAccount writes. The hosted console reads and writes no Secret through the API server. | [External clusters](external-clusters.md), reviewed on 2026-10-21 |
| §2: a capability check never stands in for authorization. | Quarantine, removal and issuing a channel credential are written by the console's ServiceAccount after a `SelfSubjectAccessReview` says the person may delete the entry. Administrators cannot edit a fence the console wrote, so an edit cannot lift it. The exception reaches only the registry in the console's namespace. | [External clusters](external-clusters.md), reviewed on 2026-10-21 |
| §7: no Console API writes RBAC. | Activating an enrollment creates a Role and a RoleBinding in the console's namespace, through the connection administrator's own client, that let the enrolled accounts `get` that one `ConsoleCluster`. `connection-admin` holds `create` on Roles and RoleBindings there, and the API server refuses a grant the administrator does not hold. | [Change what an agent allows](../hosted/external-clusters.md#change-what-an-agent-allows) |
| §12: the channel secret stays on the agent's side. | `cluster enroll` makes the first credential on the target, as proposed. A rotation's credential is made by the console and shown once to the administrator, who hands it to the target's administrator. The console keeps its verifier only. | [Rotate the channel credential](../hosted/external-clusters.md#rotate-the-channel-credential) |
| §10: structured audit on stdout. | One JSON line per action on stderr, with the message `audit`, beside the process log. | [README](../../README.md) |

## Not built

Each item below is proposed in #588 and decided no further. When one is
built, it owes the acceptance gates #588 §20 sets for it.

Phase 3, from #588 §15:

- **OIDC through agents.** A console that signs people in with OIDC reaches
  only its own cluster. #588 proposed mapping the provider's issuer and
  subject to an administrator-approved Console account, which agents would
  then impersonate as they do today. The decision of 2026-10-24 covers the
  hosting cluster only, so this stays open.
- **Native per-target credentials.** A person signs in to each target with a
  credential that target's API server accepts, and nothing is impersonated.
  Each such target has to trust the issuer. The mock-up's "Sign in to
  prod-eu-1" belongs here; see [design departures](design-departures.md).
- **A transport-only connector**, `ptah-console-connector`, for a target
  whose API server the console cannot reach. It would carry end-to-end TLS
  between the console and one fixed API server and hold no Kubernetes
  credential, RBAC or impersonation. It serves native credentials only, and
  is a different program from the agent, which does hold authority.

Phase 4, from #588 §16:

- **More than one replica.** Sessions, the decision ledger and each agent's
  channel live in one process, so the chart refuses more than one replica and
  rolls out with the Recreate strategy. #588 asks first for tested revocation
  across replicas, protected shared session storage and channel ownership.
  Sticky sessions alone are not enough.
- **Federation and integrations**: more identity providers, automatic identity
  sync, organization-wide policies, stored history and external audit sinks.
  The audit stays one line per action on the process's output, for a log
  collector to keep. The console sends no telemetry.
