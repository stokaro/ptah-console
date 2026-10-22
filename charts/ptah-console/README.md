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
  is the ServiceAccount's only grant outside the registry.
- In every namespace of `namespaces`, a Role for each role some account has
  and a RoleBinding to its accounts. `plan-reader`, `desired-state-author` and
  `diagnostic-reader` are the operator's example roles, copied from the
  operator module this console is built against by `make generate`. `viewer`
  reads every Ptah kind except the chunks that hold plan SQL. `approver`
  binds the operator chart's approver ClusterRole.
- With `registry.enabled`, in the release namespace: this cluster's
  `ConsoleCluster`, named `this-cluster-` and the first eight characters of
  its connection UID, and three Roles. The ServiceAccount may list entries and
  write their status. Every account may get this cluster's entry and its
  preferences by name, and list none. `connection-admin` accounts may create,
  change and delete entries; the role grants nothing in the namespaces of the
  ceiling.

- With `agents.enabled`, which needs the registry: the listener the agents of
  external clusters dial, on port 8444 with its own certificate from
  `agents.tls.secretName`, and a Service of its own that carries that port
  and nothing else. It serves only the agent channel, over HTTP/2 and TLS 1.3,
  and the browser's port does not serve it. With the NetworkPolicy, an empty
  `agents.ingressFrom` admits any source, since agents come from other
  clusters.

The registry's CRDs are in `crds/`. Helm installs them with the release and
never upgrades or removes them, so an installation upgraded from a chart
without them applies them first:

```sh
kubectl apply --server-side -f charts/ptah-console/crds/
```

## What it refuses

An empty account list, a namespace or role that is not one of the known
names, an account ID or login that starts with `system:`, the same ID or
login twice, `cluster-admin`, `admin`, `edit` or a `system:` role as the
approver role, more than one replica, a public URL that is not the `https`
root of a host, anything but exactly one TLS choice, and `trustedProxies`
without an enabled NetworkPolicy whose `ingressFrom` names the proxies.

A changed password takes effect when the Pod restarts:

```sh
kubectl -n ptah-console rollout restart deployment/console-ptah-console
```
