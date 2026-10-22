# Security model

The console is a client of the Kubernetes API. It decides nothing about what
a person may do: the API server's RBAC and the operator's admission decide,
for the Kubernetes identity the person's requests run as. This page says
which identity that is in each mode, what the console holds, and what it
cannot contain.

## Whom requests run as

**Local mode.** `ptah-console local` runs on the person's machine with their
own kubeconfig context, resolved once at start. Every request runs as that
context's identity. Nothing is installed in the cluster, and the console holds
no credential the kubeconfig did not already hold. The browser talks to a
loopback listener that refuses any other Host or Origin, and signs in with a
link that works once, for a minute. [Authentication](design/authentication.md)
has the details.

**Hosted mode.** `ptah-console serve` runs in the cluster, from the chart in
`charts/ptah-console`. People sign in with a Console account: a login and a
password whose bcrypt hash is in a Secret. Each account has exactly one
Kubernetes username, built by the server from the configuration alone:
`ptah-console:<installationID>:<accountID>`. The console's ServiceAccount
impersonates that username for every request the person makes, and nothing
from the browser takes part in choosing it. Requests that carry
`Impersonate-*` or `X-Remote-*` headers are refused.

What admission records on an approval or an acknowledgment is the identity
the request ran as: the kubeconfig's user in local mode, the account's
username in hosted mode.

## What the console holds

- No Secret's value, ever. The console reads no Secret, no ConfigMap of the
  operator's and no `PtahResultRecord`; it shows a database Secret by name
  only.
- Sessions in the memory of the one process: an opaque cookie,
  `HttpOnly`, `SameSite=Strict`, host-only, and in hosted mode `Secure` with
  the `__Host-` prefix. A session ends after 30 minutes without an action, 8
  hours after sign-in, on sign-out, and when the process restarts.
- In hosted mode, the bcrypt hashes from the passwords Secret and the
  ServiceAccount token Kubernetes mounts.
- Nothing on disk in hosted mode. Local mode keeps one state file, mode 0600,
  with the credential plugins the person agreed to run.

## The limit of impersonation

Hosted mode uses classic Kubernetes impersonation, restricted to exact
usernames. It is not constrained impersonation, and the difference matters:

- The ServiceAccount's grant is cluster-scoped `impersonate` on `users`,
  limited by `resourceNames` to the accounts' usernames. The chart renders
  that list and nothing broader: no groups, UIDs, extras, ServiceAccounts or
  `system:` names, and an empty account list fails installation instead of
  rendering an unrestricted rule.
- The grant permits **all** effective rights of each named username. The
  ServiceAccount's own permissions are not intersected with the person's.
  What an account can do is what RBAC grants its username, including what it
  inherits through `system:authenticated` and any external authorizer.
- A username prefix is not an authorization boundary. Containment depends on
  keeping each account's grants narrow. The chart binds only the Ptah roles
  you choose, in the namespaces you choose. Never enroll an administrator
  identity for convenience, and never bind an account to `cluster-admin`,
  `admin`, `edit` or a wildcard role; the chart refuses the first three and
  `system:` roles as the approver role.
- A compromised console, or anyone who can run code in its Pod or read its
  ServiceAccount token, can act with the union of every enrolled account's
  rights. Restrict who may create Pods, exec into them, or request tokens in
  the console's namespace to the console's administrators.
- An impersonated identity proves which Console account approved, not that
  two different people were involved: one person can hold two accounts, or a
  Console account and a kubeconfig that name them differently. Where distinct
  approval matters, keep one canonical identity per person and keep the
  operator's `approvals.requireDistinctApprover` on. The console never merges
  identities by email or display name.

## The registry

With the chart's `registry.enabled`, the ServiceAccount may also list the
entries of `console.ptah.run` in the console's namespace, get them, and write
their status. It uses that list for names only: the console shows a person an
entry, a count or a status only after the person's own `get` of that entry
succeeds, so an entry the person was not granted by name leaves no trace. An
account may get this cluster's entry and list none; creating, changing and
deleting entries takes the `connection-admin` role.

At startup the console reads this cluster's entry and refuses to serve when
its connection UID, profile, installation ID or namespace ceiling disagrees
with the configuration it was given. CEL keeps those fields immutable, so
changing one is a new entry rather than an edit.

The console's own namespace and typed allowlist do not change any of this:
they keep a working console from asking for more than it shows, and do not
contain a malicious binary holding the token.

## Network

- Hosted mode serves HTTPS only: its own TLS, or plain HTTP behind a proxy you
  declare terminates TLS. It answers only to the host in `publicURL`.
- Sign-in attempts are limited per login and per client address. Behind a
  proxy, name the proxies in `trustedProxies` so the client address comes
  from `X-Forwarded-For`; see [Behind a proxy](hosted/serve.md#behind-a-proxy).
  The chart then requires its NetworkPolicy, with `ingressFrom` naming the
  proxies, so nothing else in a trusted network can reach the console and
  name a client.
- The console opens connections to the Kubernetes API server and nowhere else.
  The chart's NetworkPolicy admits the peers you name and lets the console
  reach only the API server's addresses.
