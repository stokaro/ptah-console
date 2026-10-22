#!/bin/sh
# Validates the chart's renders against the API schemas of every Kubernetes
# minor the operator supports, read from the operator module go.mod requires.
# The renders are the value files the chart tests use: the least an
# installation gives, and everything the chart can render.
set -eu
cd "$(dirname "$0")/.."

helm() { go tool -modfile=tools/chart/go.mod helm "$@"; }
kubeconform() { go tool -modfile=tools/chart/go.mod kubeconform "$@"; }

support="$(go mod download -json github.com/stokaro/ptah-operator | jq -er .Dir)/support/kubernetes.json"
minors=$(jq -r '.releases[].minor' "$support")
[ -n "$minors" ] || {
	echo "chart-conform: $support names no Kubernetes minor" >&2
	exit 1
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/renders"
renders=0
for values in test/chart/testdata/*.values.yaml; do
	name=$(basename "$values" .values.yaml)
	helm template console charts/ptah-console --namespace ptah-console --values "$values" >"$work/renders/$name.yaml"
	renders=$((renders + 1))
done
[ "$renders" -gt 0 ] || {
	echo "chart-conform: no value files under test/chart/testdata" >&2
	exit 1
}

# The registry's entries are checked against the schema of the chart's own
# CRDs. The HTTPRoute is a Gateway API CRD, whose schema is in the CRDs
# catalog.
go run ./hack/crdschemas charts/ptah-console/crds "$work/schemas"
conform() {
	kubeconform -strict -summary -kubernetes-version "$1.0" \
		-schema-location default \
		-schema-location "$work/schemas/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json" \
		-schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
		"$2"
}
echo "$minors" | while read -r minor; do
	echo "chart-conform: Kubernetes $minor"
	conform "$minor" "$work/renders"
done

# A field the registry's schema does not declare is refused, so the check
# above read that schema rather than passing the entries unexamined.
mkdir "$work/refused"
cat >"$work/refused/entry.yaml" <<'ENTRY'
apiVersion: console.ptah.run/v1alpha1
kind: ConsoleCluster
metadata: {name: refused, namespace: ptah-console}
spec:
  connectionUID: 3f2c4b1e-8d0a-4c2e-9b7f-1a2b3c4d5e6f
  profile: CoLocated
  installationID: prod
  namespaces: [storefront]
  configurationOwner: GitOps
  ceiling: [storefront]
ENTRY
minor=$(echo "$minors" | tail -n 1)
if refusal=$(conform "$minor" "$work/refused" 2>&1); then
	echo "chart-conform: a ConsoleCluster with an undeclared field passed" >&2
	exit 1
fi
echo "$refusal" | grep -q "additional properties 'ceiling' not allowed" || {
	echo "chart-conform: the undeclared field was not what refused the entry: $refusal" >&2
	exit 1
}
