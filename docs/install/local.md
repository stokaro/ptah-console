# Run the console on your workstation

`ptah-console local` runs on your machine with your own kubeconfig. Nothing is
installed in the cluster, and every request runs as the context you choose:
the API server's RBAC and the operator's admission decide what you may see and
do, as they would for `kubectl`.

## What you need

- A cluster with Ptah Operator installed. [Compatibility](../compatibility.md)
  says which operator and Kubernetes versions this console works with.
- A kubeconfig context whose identity may read the Ptah resources in the
  namespaces you want to see. The operator ships example roles in its
  `examples/` directory: `approver-plan-reader-role.yaml` to read plans and
  their SQL, `desired-state-author-role.yaml` to edit schemas and migrations,
  `diagnostic-reader-role.yaml` to read Jobs, Pods, logs and Events, and its
  chart's approver ClusterRole to approve. The console offers what your
  identity may do and nothing more.

## Install

Download the archive for your platform from the
[releases](https://github.com/stokaro/ptah-console/releases), check it, and put
the binary on your `PATH`. On Linux amd64:

```sh
version=0.1.0
gh release download "v$version" --repo stokaro/ptah-console \
  --pattern "ptah-console_${version}_linux_amd64.tar.gz" --pattern SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
gh attestation verify "ptah-console_${version}_linux_amd64.tar.gz" \
  --repo stokaro/ptah-console --signer-workflow stokaro/ptah-console/.github/workflows/release.yml
tar -xzf "ptah-console_${version}_linux_amd64.tar.gz" ptah-console
install -m 0755 ptah-console ~/.local/bin/
```

The archives for macOS end in `_darwin_amd64.tar.gz` or `_darwin_arm64.tar.gz`,
and the Windows ones are `.zip`. `ptah-console version` names the version and
says the web app is in it.

## Run

```sh
ptah-console local --context production --namespace storefront
```

The console pins the context when it starts: changing the kubeconfig's
current context afterwards does not move it. It listens on the loopback
address only, opens your browser with a sign-in link that works once, for one
minute, and prints the link instead with `--no-open`. Press Enter in the
terminal for a new link. Stop it with Ctrl-C.

- `--namespace` names a namespace to show; repeat it for more, up to 64.
  Without it the console shows the context's namespace, or the first 64
  namespaces your identity may list, and says when it cut the list. In the
  full console you choose which of them the pages read, from the top bar.
- `--read-only` refuses every change the console could send, whatever your
  identity may do.
- `--kubeconfig` and `--context` choose another kubeconfig or context.
- `--port` fixes the loopback port; by default the console takes a free one.
- A context that runs a credential plugin, such as a cloud provider's login
  helper, asks once in the terminal before the plugin first runs, and
  remembers the answer. `--trust-exec-plugin` answers yes without asking.

Audit lines, one per decision the console sent, go to standard error.

## Remove

Delete the binary. The console keeps one file, the credential plugins you
agreed to run, at `ptah-console/state.json` under your user config directory
(`~/.config` on Linux, `~/Library/Application Support` on macOS,
`%AppData%` on Windows); delete it too. Nothing in the cluster changes.
