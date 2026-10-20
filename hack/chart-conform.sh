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
renders=0
for values in test/chart/testdata/*.values.yaml; do
	name=$(basename "$values" .values.yaml)
	helm template console charts/ptah-console --namespace ptah-console --values "$values" >"$work/$name.yaml"
	renders=$((renders + 1))
done
[ "$renders" -gt 0 ] || {
	echo "chart-conform: no value files under test/chart/testdata" >&2
	exit 1
}

# The HTTPRoute is a Gateway API CRD, whose schema is in the CRDs catalog.
echo "$minors" | while read -r minor; do
	echo "chart-conform: Kubernetes $minor"
	kubeconform -strict -summary -kubernetes-version "$minor.0" \
		-schema-location default \
		-schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json' \
		"$work"/*.yaml
done
