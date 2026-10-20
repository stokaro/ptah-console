# ptah-console chart

Installs a hosted Ptah Console in a namespace of its own, beside an installed
Ptah Operator. People sign in with Console accounts, and each account acts in
Kubernetes as `ptah-console:<installationID>:<id>` through the console's
ServiceAccount, which may impersonate exactly those usernames.

```sh
kubectl create namespace ptah-console
kubectl -n ptah-console create secret generic ptah-console-passwords --from-file=passwords=./passwords
kubectl -n ptah-console create secret tls ptah-console-tls --cert=tls.crt --key=tls.key
helm install console ./charts/ptah-console --namespace ptah-console --values values.yaml
```

with values such as:

```yaml
installationID: prod
publicURL: https://console.example.com
namespaces: [storefront, billing]
accounts:
  - id: alex
    login: alex@example.com
    displayName: Alex Morgan
    roles: [viewer, approver]
  - id: sam
    login: sam@example.com
    roles: [desired-state-author, plan-reader]
passwords:
  secretName: ptah-console-passwords
tls:
  secretName: ptah-console-tls
```

[values.yaml](values.yaml) describes every value. The passwords file and its
hashes are described in [Hosted accounts](../../docs/hosted/accounts.md), and
the server in [Serving a hosted console](../../docs/hosted/serve.md).

## What it renders

- One Deployment, one replica, the `Recreate` strategy. More replicas are
  refused, because sessions live in the replica's memory. The Pod runs as a
  non-root user with a read-only root filesystem, every capability dropped and
  the `RuntimeDefault` seccomp profile.
- A Service, and when enabled an Ingress, an HTTPRoute and an example
  NetworkPolicy.
- A ConfigMap with the configuration. The passwords and the TLS certificate
  stay in Secrets you create; the chart only names them.
- A ClusterRole that grants `impersonate` on `users`, limited by
  `resourceNames` to the accounts' usernames, bound to the ServiceAccount. It
  is the ServiceAccount's only grant.
- In every namespace of `namespaces`, a Role for each role some account has
  and a RoleBinding to its accounts. `plan-reader`, `desired-state-author` and
  `diagnostic-reader` are the operator's example roles, copied from the
  operator module this console is built against by `make generate`. `viewer`
  reads every Ptah kind except the chunks that hold plan SQL. `approver`
  binds the operator chart's approver ClusterRole.

## What it refuses

An empty account list, a namespace or role that is not one of the known
names, an account ID or login that starts with `system:`, the same ID or
login twice, `cluster-admin`, `admin`, `edit` or a `system:` role as the
approver role, more than one replica, a public URL that is not the `https`
root of a host, and anything but exactly one TLS choice.

A changed password takes effect when the Pod restarts:

```sh
kubectl -n ptah-console rollout restart deployment/console-ptah-console
```
