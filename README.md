# Ptah Console

An optional web console for [Ptah Operator](https://github.com/stokaro/ptah-operator).

It answers the questions a person has when a schema change does not reach the
database on its own: what is happening, why, what is waiting for me, and which
action is valid next. A person can review the exact SQL of a plan and approve
that plan under their own Kubernetes identity; the operator's admission and
reconciliation stay the authority.

The console is not required to run the operator, and removing it does not
affect anything the operator does.

## Status

Under construction; there is no release yet. The plan and its steps are in
[stokaro/ptah-operator#587](https://github.com/stokaro/ptah-operator/issues/587),
and the architecture and security contract is
[stokaro/ptah-operator#588](https://github.com/stokaro/ptah-operator/issues/588).

## Modes

- `ptah-console local --context <context>` runs on your workstation against
  your own kubeconfig, on a loopback listener. Nothing is installed in the
  cluster.
- Hosted: a Helm chart installs the console beside the operator, with personal
  Console accounts mapped to exact Kubernetes identities through impersonation.

## Building

```bash
make build
./bin/ptah-console version
```

## License

[MIT](LICENSE).
