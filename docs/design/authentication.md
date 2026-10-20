# Authentication

Two questions are kept apart: who the person is to the console, and which
Kubernetes identity their requests run as. The API server and the operator's
admission decide what that identity may do; the console decides nothing about
it.

## Local mode

```sh
ptah-console local --context production --namespace application
```

The person runs the console on their own machine with their own kubeconfig.

- The context is resolved once, at start, and pinned. A later change to the
  kubeconfig's `current-context` does not move the console.
- A context that runs an exec credential plugin needs confirmation in the
  terminal the first time. The trust is recorded in a state file under the XDG
  config directory, mode 0600 in a 0700 directory. No plugin runs to list
  contexts.
- The listener is loopback only, IPv4 and IPv6.
- The browser is opened with a launch token in the URL fragment: 256 bits,
  single use, valid for 60 seconds. The page exchanges it for a session cookie
  and removes it from history. A fragment is never sent to a server, so the
  token never reaches an access log.
- Every request has to carry the exact Host and Origin of the listener. A page
  on another origin, or one that rebinds a DNS name to 127.0.0.1, is refused.
- `--read-only` refuses every mutation on the server. It does not make the
  kubeconfig's credential any weaker outside the console.
- A failure to authenticate never falls back to another context or to
  in-cluster credentials.
- No kubeconfig content, token, key or plugin output reaches the browser.

## Hosted mode

The console runs beside the operator with personal Console accounts.

- Accounts are configured by the installer: an immutable ID, a login and a
  display name. Passwords are an htpasswd file in a Secret, bcrypt only; other
  hash formats are refused, and so is a password longer than bcrypt's 72 bytes
  rather than truncating it. [Hosted accounts](../hosted/accounts.md) gives
  both file formats and their rules.
- Each account maps to exactly one Kubernetes username, built on the server:
  `ptah-console:<installationID>:<accountID>`. Nothing from the browser takes
  part in it.
- The console's ServiceAccount may impersonate exactly those usernames, through
  RBAC `resourceNames`: no groups, no UIDs, no extras, no ServiceAccounts, no
  `system:` names. An empty list fails installation rather than rendering an
  unrestricted rule.
- Each mapped username gets RoleBindings to Ptah roles in the namespaces the
  installer chose, and nothing else.
- Sign-in is rate limited per account and per source address, answers an
  unknown login exactly like a wrong password, and rotates the session.
- HTTPS is required: the console's own TLS, or TLS terminated upstream and
  declared in configuration. Without one of them the server does not start.

### What impersonation does not contain

This is classic impersonation restricted to exact usernames. The grant is
cluster-scoped and allows everything each named identity may do; the
ServiceAccount's rights are not intersected with the person's. A compromised
console can act with the union of its enrolled identities' rights. Keep those
identities' grants narrow, including what they inherit through
`system:authenticated`, and never enroll an administrator for convenience.

The impersonated identity is what approval admission records. It proves which
Console account approved, not that two physical people were involved; the
operator's `approvals.requireDistinctApprover` setting is unchanged and still
the control for that.

## Sessions

- Server-side, in memory, behind an opaque cookie: HttpOnly, SameSite=Strict,
  host-only; in hosted mode also `Secure` with the `__Host-` prefix.
- Idle timeout 30 minutes, absolute limit 8 hours. Background refreshes do not
  count as activity.
- Logout, expiry, an account change or a rollout ends the session and closes
  its streams. One replica holds the sessions, so a restart signs everyone out.

## Requests the console refuses outright

- A cross-origin request, or one with the wrong Host.
- A request that carries `Impersonate-*` or `X-Remote-*` headers or an actor
  field: the identity comes from the session and the server's configuration
  only.
- Anything outside the fixed Connect services: there is no generic Kubernetes
  path, method or patch.
