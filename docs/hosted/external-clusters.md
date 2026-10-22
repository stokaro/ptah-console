# External clusters

A hosted console reaches clusters other than the one it runs in through an
agent installed in each of them. The agent dials the console; the target
cluster accepts no inbound connection for it, and its API server needs no
setup for the console, no OIDC issuer among it.

This page is for two people: the console's installation administrator, who
enrolls and removes connections, and each target cluster's administrator, who
installs the agent and decides what it may do there.

## What the target trusts

> The agent acts in this cluster as each Console account the target
> administrator allows, with the Kubernetes rights those usernames have here.
> A compromised console can make requests as those accounts. A compromised
> agent can use those accounts' rights in this cluster. Keep the allowlist
> short and the bound roles to Ptah's resources, and remove the agent's
> impersonation binding to cut it off at once, whatever the console says.

The agent's only grant is to impersonate the allowlisted accounts'
usernames, `ptah-console:<installationID>:<account>`. It reads no Secret, not
even its own credential, which the kubelet mounts, and writes no RBAC object.
It calls only its own cluster's API server, with the typed requests the
console's own pages make, and is not a proxy. A new Console account reaches
no target until that target's allowlist names it.

## Prepare the console

The console needs the registry and the agent listener. In its values:

```yaml
registry:
  enabled: true
agents:
  enabled: true
  # What an enrollment file tells agents: the https root they dial, the name
  # the listener's certificate carries, and the console's own CA, in PEM.
  address: https://agents.console.example.com
  serverName: agents.console.example.com
  caBundle: |
    -----BEGIN CERTIFICATE-----
    ...
    -----END CERTIFICATE-----
  tls:
    # A kubernetes.io/tls Secret with the listener's certificate from that CA.
    secretName: console-agents-tls
  service:
    type: LoadBalancer
```

The listener has a port, a Service and a certificate of its own, apart from the
browser's. Agents trust that CA and that name and nothing else, so a
certificate a public CA issued for the same name is refused. How the Service
reaches the other clusters, a LoadBalancer or an Ingress that passes TLS
through, is your choice.

The registry's CRDs come with the chart. An installation upgraded from a chart
without them applies them first:

```sh
kubectl apply --server-side -f charts/ptah-console/crds/
```

Only installation administrators enroll and change connections: give them the
`connection-admin` role in the console's values.

## Enroll a cluster

1. **In the console**, open Admin, then Add cluster. Name the connection, the
   namespaces it covers on the target, and each account it allows with its
   roles there. Create the enrollment and download its file at once: the file
   holds a single-use secret valid for 10 minutes, and the console keeps only
   its verifier. Hand the file over as a file, never in a chat message, a URL
   or a Helm value.
2. **On the target**, with your own kubeconfig, run the helper with the
   context named explicitly:

   ```sh
   ptah-console cluster enroll --context prod-ap-2 --enrollment-file ./prod-ap-2.enroll
   ```

   It asks the API server whether you may install the agent, then shows what
   will be installed and granted, with the enrollment's fingerprint. Compare
   the fingerprint with the one the console showed, and type the cluster's
   name to go on. The helper writes the channel credential into the agent's
   Secret, completes the enrollment on the console, writes the agent chart's
   values, and prints the Helm command that installs the agent. The credential
   never leaves the cluster, and nothing of your kubeconfig is sent anywhere.
3. **On the target**, run the Helm command it printed:

   ```sh
   helm upgrade --install ptah-console-agent oci://ghcr.io/stokaro/charts/ptah-console-agent \
     --version <version> --namespace ptah-console-agent --values prod-ap-2-agent-values.yaml
   ```

4. **In the console**, the enrollment shows the agent connected. Nothing is
   sent to it yet. Review its grants and activate it; the connection then
   appears in the cluster switcher for the accounts allowed there.

A helper run that stopped part way runs again with the same file: it reuses
the credential it wrote and reports the same thing. An enrollment nobody
completed within 10 minutes, or nobody activated within a day, is removed;
start a new one.

## Change what an agent allows

The allowlist and the roles are the agent chart's values and nothing else.
Change `accounts` and upgrade the release:

```sh
helm upgrade ptah-console-agent oci://ghcr.io/stokaro/charts/ptah-console-agent \
  --namespace ptah-console-agent --reuse-values --values accounts.yaml
```

The agent reads a changed allowlist without a restart. Removing an account
ends its requests at once; adding one takes effect on its next request.

The console shows a connection to the accounts its enrollment named: on
activation it binds their usernames to a Role that may get that connection's
entry and nothing else. An account added to the allowlist later needs the same
binding before the connection appears for it; an installation administrator
makes it:

```sh
kubectl --namespace <the console's namespace> create rolebinding <name> \
  --role ptah-console-connection-<the connection UID> --user ptah-console:<installationID>:<account>
```

## Upgrade the agent

Upgrade the release to the chart version of the console:

```sh
helm upgrade ptah-console-agent oci://ghcr.io/stokaro/charts/ptah-console-agent \
  --version <version> --namespace ptah-console-agent --reuse-values
```

## Rotate the channel credential

A credential is accepted for 90 days. From 14 days before it expires, the
connection says its rotation is due.

1. In the console, open the connection and issue a new credential. It is shown
   once; the console keeps its verifier.
2. On the target, write it into the agent's Secret:

   ```sh
   kubectl --namespace ptah-console-agent create secret generic ptah-console-agent-credential \
     --from-literal=credential=<the new credential> --dry-run=client --output yaml \
     | kubectl apply --filename -
   ```

The agent notices the changed Secret and opens its channel again with the new
credential. The console then retires the old one an hour later at most.

## Disable, quarantine and remove

- **Disable** stops every request to the connection and keeps its
  configuration. Enable it again at any time.
- **Quarantine** closes the channel and refuses the connection's credentials,
  even if its record is applied again from Git. To lift it, issue a new
  credential: it comes at a new generation, which the agent's installation
  has to name.

  ```sh
  kubectl --namespace ptah-console-agent create secret generic ptah-console-agent-credential \
    --from-literal=credential=<the new credential> --dry-run=client --output yaml \
    | kubectl apply --filename -
  helm upgrade ptah-console-agent oci://ghcr.io/stokaro/charts/ptah-console-agent \
    --namespace ptah-console-agent --reuse-values --set connection.generation=<the new generation>
  ```

- **Remove** revokes the connection first, then removes it from the console,
  and keeps a tombstone that refuses its credentials if its record comes back.
  It does not reach the target, so it works on an offline cluster too, and the
  target's cleanup stays pending. A name reused later is a new connection.

None of these stops the operator, its plans, approvals or running operations,
or touches any application or database.

## Uninstall the agent

On the target, after the connection is removed in the console. Removing the
impersonation binding first ends the agent's authority at once:

```sh
kubectl delete clusterrolebinding ptah-console-agent-impersonate
helm uninstall ptah-console-agent --namespace ptah-console-agent
ptah-console cluster unenroll --context prod-ap-2 --connection-uid <the connection UID>
```

`unenroll` removes the credential Secret the helper made, and only for that
connection. The namespace stays: delete it yourself if nothing else is in it.

## Backup and restore

The console's record of a connection lives in its namespace: the registry
entry, the verifier of each credential in its status, and the revocations that
fence quarantined and removed connections. Restoring that namespace from a
backup restores those records as they were when it was taken:

- a credential rotated since then is accepted again, until it expires;
- a connection quarantined or removed since then is accepted again, unless its
  revocation is in the backup.

Restore the console isolated, with `agents.enabled: false`, issue a new
credential for every connection, have each target install it, and only then
enable the agent listener. Enroll again any connection removed since the
backup.

No software detects that all of its revocation state was rolled back at once.
If the registry and its revocations go back together, an old credential is
valid again until it expires, at most 90 days after it was issued. The target
administrator's own cut-off does not depend on the console: removing the
agent's impersonation binding on the target ends its authority whatever the
console holds.
