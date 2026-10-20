# Releases

A release of the console publishes:

- `ptah-console` for Linux, macOS and Windows on amd64 and arm64, with the
  web app in it, as `ptah-console_<version>_<os>_<arch>.tar.gz` (`.zip` on
  Windows), with an SPDX SBOM beside each archive;
- `SHA256SUMS` for the archives;
- the image `ghcr.io/stokaro/ptah-console:<version>` for `linux/amd64` and
  `linux/arm64`, on distroless as a non-root user, with its own SBOM;
- the chart, as the release asset `ptah-console-<version>.tgz` and as the OCI
  artifact `oci://ghcr.io/stokaro/charts/ptah-console`.

Each of them carries a build provenance attestation from the release
workflow, and the workflow verifies every one before it publishes the
release. [support/compatibility.json](../support/compatibility.json) names
the operator version and the Kubernetes minors each console version was built
and tested against; a release refuses to build against an operator that is
not itself a release.

## Verify before you install

```sh
version=0.1.0
repository=stokaro/ptah-console
workflow="$repository/.github/workflows/release.yml"

gh release download "v$version" --repo "$repository" \
  --pattern "ptah-console_${version}_linux_amd64.tar.gz" --pattern SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
gh attestation verify "ptah-console_${version}_linux_amd64.tar.gz" \
  --repo "$repository" --signer-workflow "$workflow"

gh attestation verify "oci://ghcr.io/stokaro/ptah-console:$version" \
  --repo "$repository" --signer-workflow "$workflow"
gh attestation verify "oci://ghcr.io/stokaro/charts/ptah-console:$version" \
  --repo "$repository" --signer-workflow "$workflow"
```

Install the image by the digest you verified rather than by its tag.

## Making a release

1. Set the version in `charts/ptah-console/Chart.yaml` (`version` and
   `appVersion`) and add a row for it at the top of
   `support/compatibility.json`, naming the released operator `go.mod`
   requires. `make release-check` holds them to each other.
2. Merge, and let CI pass on that commit.
3. Run the release workflow by hand on `main`. That is a dry run: the same
   build, attestations and checks, with nothing published.
4. Push the tag `v<version>` on that commit. The workflow builds, attests,
   verifies and publishes.
