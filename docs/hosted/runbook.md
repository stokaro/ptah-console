# Running a hosted console

The everyday changes to a hosted console's accounts, and what each one takes.
Every change goes through the chart's values and the passwords Secret; the
running console cannot add an account, change its own grants or edit the
list of usernames it may impersonate.

A rollout restarts the one replica and signs everyone out. That is also what
makes a changed Secret take effect:

```sh
kubectl -n ptah-console rollout restart deployment/console-ptah-console
```

## Add an account

1. Choose an `id` that has never been used in this installation. It becomes
   part of the Kubernetes username, `ptah-console:<installationID>:<id>`, and
   RBAC bindings name it, so it never changes and is never reused for another
   person.
2. Make a password hash:

   ```sh
   ptah-console accounts hash
   ```

3. Add `<id>:<hash>` as a line of the passwords Secret.
4. Add the account to `accounts` in your values, with its `login`,
   `displayName` and `roles`.
5. Check both files the way the server will, then upgrade the release. Helm
   updates the impersonation rule and the RoleBindings, and the changed
   configuration rolls the Pod:

   ```sh
   ptah-console config validate --config configuration.yaml --passwords passwords
   helm upgrade console ./charts/ptah-console --namespace ptah-console --values values.yaml
   ```

## Reset a password

Replace the account's line in the passwords Secret with a new hash, then roll
the console out. The old password stops working when the new Pod starts, and
every session ends with the old Pod.

## Disable an account

Remove the account from `accounts` and its line from the passwords Secret,
then upgrade the release. The upgrade removes the username from the
impersonation rule and from the RoleBindings, so the console cannot act as it
even if a session survived, and the rollout ends every session. Keep the `id`
recorded as used; never give it to someone else.

The console refuses to start with an account that has no password line, so
the two go together.

## Change what an account may do

Change its `roles` and upgrade the release. The RoleBindings change at once,
and the account's next request runs under the new grants without a restart.
Changing the installation's `namespaces` changes the configuration as well,
so that upgrade also rolls the console out.

## Rotate the console's credentials

- **TLS certificate.** Replace the certificate in its Secret. The console reads
  the files again when they change, so the next connection gets the new one
  without a restart. With `tls.terminatedUpstream`, the proxy's certificate is
  the proxy's business.
- **ServiceAccount token.** Kubernetes rotates the projected token the Pod
  mounts, and the Kubernetes client reads the file again as it changes.
- **Password hashes.** Rotating a password is a reset, above.

## Check what an account may do

The username is an ordinary Kubernetes user, so the API server answers for it:

```sh
kubectl auth can-i --list --as ptah-console:prod:alex --namespace storefront
```

It should list Ptah resources and, for a diagnostic reader, Jobs, Pods, their
logs and Events, and nothing else.
