# Install a hosted console

A hosted console runs in the cluster, beside the operator, for people who
sign in with a Console account. Each account acts in Kubernetes as its own
username, `ptah-console:<installationID>:<id>`, through the console's
ServiceAccount, which may impersonate exactly those usernames. Read
[Security model](../security.md) first: it says what that grant allows and
what it cannot contain.

## What you need

- Ptah Operator installed, and its chart's approver ClusterRole if any account
  is to approve. [Compatibility](../compatibility.md) names the versions.
- A namespace for the console of its own, never the operator's.
- An HTTPS address people will open, such as `https://console.example.com`,
  and either a certificate for it or an Ingress or Gateway that terminates
  TLS for it.
- Helm 3.8 or later, or Helm 4.

## 1. Make the passwords file

One line per account, the account ID, a colon and a bcrypt hash.
`ptah-console accounts hash` makes the hash; at a terminal it asks twice and
does not echo:

```sh
printf 'alex:%s\n' "$(ptah-console accounts hash)" >> passwords
printf 'sam:%s\n' "$(ptah-console accounts hash)" >> passwords
kubectl create namespace ptah-console
kubectl -n ptah-console create secret generic ptah-console-passwords --from-file=passwords=passwords
```

[Hosted accounts](../hosted/accounts.md) gives the rules for IDs, logins and
hashes.

## 2. Choose how HTTPS is served

Either the console serves TLS with a certificate in a Secret, which it reads
again when the Secret is renewed:

```sh
kubectl -n ptah-console create secret tls ptah-console-tls --cert=tls.crt --key=tls.key
```

or an Ingress or Gateway terminates TLS and the console serves plain HTTP to
it, which you declare with `tls.terminatedUpstream: true`. Behind a proxy,
also name the proxies' networks in `trustedProxies`, so sign-in attempts are
counted per client; the chart then requires the NetworkPolicy, with
`ingressFrom` naming the proxies. [Serving a hosted
console](../hosted/serve.md#behind-a-proxy) explains why.

## 3. Write the values

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

`roles` are `viewer`, `plan-reader`, `desired-state-author`,
`diagnostic-reader` and `approver`; [the chart's
README](../../charts/ptah-console/README.md) says what each grants, and its
`values.yaml` describes every value. `approverClusterRole` names the operator
chart's approver ClusterRole, `ptah-operator-ptah-operator-approver` for an
operator released as `ptah-operator`.

## 4. Install

```sh
helm install console oci://ghcr.io/stokaro/charts/ptah-console --version 0.1.0 \
  --namespace ptah-console --values values.yaml
kubectl -n ptah-console rollout status deployment/console-ptah-console
```

If ghcr.io asks you to sign in, run `helm registry login ghcr.io` with your
GitHub account first. The cluster pulls the image itself, so if ghcr.io asks
it for credentials too, either pull the image from its Docker Hub mirror,
which serves the same digest to anyone, with
`image.repository: docker.io/stokaro/ptah-console` (Docker Hub limits
anonymous pulls), or give the cluster a pull Secret made from a GitHub token
with `read:packages`, and name the Secret in the values as
`imagePullSecrets: [{name: ghcr-pull}]`:

```sh
kubectl -n ptah-console create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io --docker-username="$github_user" --docker-password="$github_token"
```

The chart refuses values it cannot install safely: no accounts, a wildcard or
unknown role, a `system:` name, an administrator ClusterRole as the approver
role, more than one replica, a public URL that is not the `https` root of a
host, or anything but one way of serving HTTPS. Expose the Service with the
chart's Ingress or HTTPRoute, or your own.

Check what each account may do; it should be Ptah resources and nothing else:

```sh
kubectl auth can-i --list --as ptah-console:prod:alex --namespace storefront
```

Open the public address and sign in.

## Day to day

[Running a hosted console](../hosted/runbook.md) covers adding, resetting and
disabling accounts, changing their roles, and rotating the certificate.
[External clusters](../hosted/external-clusters.md) covers enrolling, rotating
and removing the clusters the console reaches through an agent.

## Uninstall

```sh
helm uninstall console --namespace ptah-console
kubectl delete namespace ptah-console
```

This removes the console, its impersonation grant and the accounts'
RoleBindings, and with the namespace the passwords and TLS Secrets. The
operator is not affected: approvals already admitted stay admitted and are
applied as before.

Helm leaves the registry's CRDs in place. Removing them deletes every entry
in every namespace, so do it only when no console uses them:

```sh
kubectl delete crd consoleclusters.console.ptah.run consoleclusterpreferences.console.ptah.run
```
