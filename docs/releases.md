# Releases

A release of the console publishes:

- `ptah-console` for Linux, macOS and Windows on amd64 and arm64, with the
  web app in it, as `ptah-console_<version>_<os>_<arch>.tar.gz` (`.zip` on
  Windows), with an SPDX SBOM beside each archive;
- `SHA256SUMS` for the archives;
- the image `ghcr.io/stokaro/ptah-console:<version>` for `linux/amd64` and
  `linux/arm64`, on distroless as a non-root user, with its own SBOM, and the
  same digest at `docker.io/stokaro/ptah-console:<version>`, which anyone can
  pull without signing in;
- the chart, as the release asset `ptah-console-<version>.tgz` and as the OCI
  artifact `oci://ghcr.io/stokaro/charts/ptah-console`;
- the agent a hosted console reaches other clusters through: the image
  `ghcr.io/stokaro/ptah-console-agent:<version>` for `linux/amd64` and
  `linux/arm64`, the same digest at
  `docker.io/stokaro/ptah-console-agent:<version>`, and its chart, as the
  release asset `ptah-console-agent-<version>.tgz` and as
  `oci://ghcr.io/stokaro/charts/ptah-console-agent`. The agent is released
  with the console and carries its version.

Each of them carries a build provenance attestation from the release
workflow, and the workflow verifies every one before it publishes the
release. [support/compatibility.json](../support/compatibility.json) names
the operator version and the Kubernetes minors each console version was built
and tested against; a release refuses to build against an operator that is
not itself a release.

## Verify before you install

```sh
version=0.2.0
repository=stokaro/ptah-console
workflow="$repository/.github/workflows/release.yml"

gh release download "v$version" --repo "$repository" \
  --pattern "ptah-console_${version}_linux_amd64.tar.gz" --pattern SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
gh attestation verify "ptah-console_${version}_linux_amd64.tar.gz" \
  --repo "$repository" --signer-workflow "$workflow"

gh attestation verify "oci://ghcr.io/stokaro/ptah-console:$version" \
  --repo "$repository" --signer-workflow "$workflow"
gh attestation verify "oci://docker.io/stokaro/ptah-console:$version" \
  --repo "$repository" --signer-workflow "$workflow"
gh attestation verify "oci://ghcr.io/stokaro/charts/ptah-console:$version" \
  --repo "$repository" --signer-workflow "$workflow"
gh attestation verify "oci://ghcr.io/stokaro/ptah-console-agent:$version" \
  --repo "$repository" --signer-workflow "$workflow"
gh attestation verify "oci://ghcr.io/stokaro/charts/ptah-console-agent:$version" \
  --repo "$repository" --signer-workflow "$workflow"
```

Install the image by the digest you verified rather than by its tag.

## Making a release

1. Build against a released operator. A console release names one, and
   `make release-check` refuses a pseudo-version:

   ```sh
   go get github.com/stokaro/ptah-operator@v0.2.8
   go mod tidy
   make generate
   ```

   `make generate` copies the operator's example roles into both charts
   again; only their header line changes when the operator's examples did
   not.
2. Set the version: `version` and `appVersion` in
   `charts/ptah-console/Chart.yaml` and `charts/ptah-console-agent/Chart.yaml`,
   a row at the top of `support/compatibility.json` naming the operator
   `go.mod` requires, the same row in [compatibility](compatibility.md), and
   the version the install guides and this page download. `make release-check`
   holds the charts and the record to each other; nothing checks the guides,
   so search them for the previous version.
3. Run every acceptance row on the release branch. The workflow runs on a pull
   request only for the paths it lists, and a change to the web app is not
   among them:

   ```sh
   gh workflow run acceptance.yml --repo stokaro/ptah-console --ref <branch>
   ```

4. Merge, and let CI pass on that commit.
5. Run the release workflow by hand on `main`. That is a dry run: the same
   build, attestations and checks, with nothing published.
6. Push the tag `v<version>` on that commit. The workflow builds, attests,
   verifies and publishes, with no approval step.
