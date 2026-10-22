# ptah-console-agent

The agent lets a hosted Ptah Console reach a cluster it does not run in. It
runs in a namespace of its own in that cluster, opens one channel to the
console, outbound, and serves the console's requests there as the Console
accounts this chart allows. Nothing listens: the chart renders no Service, no
Ingress and no container port.

Phase 2 of the console is in development; this chart is not released yet.
[The external clusters design](../../docs/design/external-clusters.md) says
how the channel works and what the agent refuses.

## Values

```yaml
installationID: prod            # the console's
connection:
  uid: 0b9a8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c6d
  generation: 1
console:
  address: https://agents.console.example.com
  serverName: agents.console.example.com
  caBundle: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
credential:
  secretName: ptah-console-agent-credential
namespaces: [storefront, billing]
accounts: [alex, sam]
```

`values.yaml` describes every value. The agent trusts only `caBundle` for the
console, and only with `serverName`: a certificate a public CA issued for the
same name is refused, and the handshake fails before the credential is sent.

## What it renders

- One Deployment, one replica, the `Recreate` strategy. The Pod runs as a
  non-root user with a read-only root filesystem, every capability dropped and
  the `RuntimeDefault` seccomp profile.
- A ClusterRole that grants `impersonate` on `users`, limited by
  `resourceNames` to `ptah-console:<installationID>:<account>` for each
  account, bound to the agent's ServiceAccount. It is the agent's only grant:
  it reads no Secret, not even its own credential, which the kubelet mounts,
  and writes no RBAC object.
- A ConfigMap with the connection and the console's CA. A change to it rolls
  the agent.
- A ConfigMap with the allowlist. The agent reads a changed list without a
  restart, and refuses a list that does not parse while keeping the one in
  force.
- When enabled, a NetworkPolicy that admits nothing in and lets the agent reach
  the API server, cluster DNS and the console's listener.

A request for an account the allowlist does not name, a namespace outside
`namespaces`, or an operation the agent does not serve is refused before any
call to the API server. An account the allowlist names but this chart's grant
does not cover is refused by the API server.

## What it refuses

An empty account list, an account, namespace or installation ID that is not a
DNS label (which keeps out `system:` names and usernames), the same account
twice, a console address that is not the `https` root of a host, a CA that is
not PEM, a connection UID that is not a lowercase UUID, generation 0, no
credential Secret, and a NetworkPolicy that does not name the API server and
the console.
