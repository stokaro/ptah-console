# Sign in with OpenID Connect

A hosted console can sign people in with an OpenID Connect provider instead
of Console accounts. Each person then acts in Kubernetes with their own ID
token: the console passes it to the API server, the API server maps it to a
username and groups, and RBAC decides. The console impersonates nobody, and
its ServiceAccount gets no `impersonate` rule.

It is one or the other: a configuration has `oidc` or `accounts`. External
clusters are reached through agents that act as Console accounts, so a
console that signs people in with OIDC reaches only the cluster it runs in.

## What the API server has to trust

The API server, not the console, decides who a token stands for. It has to
trust the provider's issuer with the console's client ID as an audience,
through its `--oidc-*` flags or its
[AuthenticationConfiguration](https://kubernetes.io/docs/reference/access-authn-authz/authentication/#using-authentication-configuration):

```yaml
apiVersion: apiserver.config.k8s.io/v1
kind: AuthenticationConfiguration
jwt:
  - issuer:
      url: https://login.example.com/realms/ops
      audiences: [ptah-console]
    claimMappings:
      username:
        claim: email
        prefix: "oidc:"
      groups:
        claim: groups
        prefix: "oidc:"
```

A cluster that trusts no issuer, or another audience, refuses every token the
console sends, and the sign-in page says so: no session is opened. So does a
token without the claims the mapping reads. The console asks for `openid`
alone unless `oidc.extraScopes` names more, and many providers put `email`
and `groups` into the ID token only when their scopes are asked for: with the
mapping above, ask for `email` and `groups`.

## The provider

Register the console as a client with the authorization code flow. Its
redirect URI is the console's public address followed by
`/auth/oidc/callback`, such as `https://console.example.com/auth/oidc/callback`.
The console sends a PKCE challenge (S256) with every sign-in, so a public
client without a secret works; a provider that lists its PKCE methods and
leaves S256 out is refused when the console starts.

Ask for `offline_access`, or whatever scope the provider gives a refresh token
under, to keep a session for longer than one ID token lives. Without a
refresh token the session ends when the ID token expires.

## The values

```yaml
installationID: prod
publicURL: https://console.example.com
namespaces: [storefront, billing]
oidc:
  enabled: true
  issuerURL: https://login.example.com/realms/ops
  clientID: ptah-console
  displayName: Example SSO
  extraScopes: [email, groups, offline_access]
  # Leave secretName empty for a public client.
  clientSecret:
    secretName: ptah-console-oidc
    key: client-secret
  # The users and groups as the API server names them, after its own prefix,
  # and the roles each gets in every namespace above.
  subjects:
    - kind: Group
      name: "oidc:ptah-approvers"
      roles: [viewer, approver]
    - kind: User
      name: "oidc:sam@example.com"
      roles: [plan-reader]
tls:
  secretName: ptah-console-tls
```

`accounts` and `passwords` stay empty. The roles are the ones
[Install a hosted console](../install/hosted.md) lists, except
`connection-admin`, which belongs to the registry. A subject whose name starts
with `system:` is refused: `system:authenticated` would give the roles to every
user the cluster knows. Bindings you write yourself work as well; the chart's
are a convenience.

Check a configuration before installing it:

```sh
ptah-console config validate --config configuration.yaml
```

## Signing in

The sign-in page offers the provider. The console sends the browser there
with a `state` bound to that browser by a cookie of its own and a `nonce` the
ID token has to carry; each attempt is kept on the server for ten minutes and
used once. When the browser comes back, the console checks the ID token's
signature, issuer, audience, expiry and nonce, and then asks the API server
whom the token stands for. Only then is a session opened.

While the session lasts the console renews the ID token before it expires,
and checks each new one as it checked the first, for the same subject. A
renewal that fails, a renewal for another subject, or a token the API server
starts refusing ends the session; nothing stands in for the person's token.
Signing out ends the console's session and not the provider's.

## When a sign-in does not work

The sign-in page names what went wrong:

| The page says | What to check |
| --- | --- |
| The API server does not accept the provider's token | The API server's trust: the issuer URL exactly as the tokens name it, and the client ID among its audiences. |
| The provider's answer did not check out | The console's `issuerURL`, `clientID` and the clock; the console's log names the check that failed. |
| That sign-in was started in another window | The browser blocks cookies, or more than ten minutes passed at the provider. |
| The API server did not answer | The console's network path to the API server. |
