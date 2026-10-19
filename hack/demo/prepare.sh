#!/bin/sh
# Prepares the operator's demonstration lab for the console's demo: a
# schema that waits for an approval, and a kubeconfig for an approver who may
# do nothing else.
#
# The lab is the operator's own (`make demo-up` in a checkout of
# stokaro/ptah-operator at the version this module requires). This script
# takes from it only what a reader would bring: the cluster, the registry and
# its credentials, and the chart's approver ClusterRole.
#
#   OPERATOR_DIR   the operator checkout whose lab is up
#   OUT_DIR        where the approver's kubeconfig and the facts are written
set -eu

OPERATOR_DIR=${OPERATOR_DIR:?set OPERATOR_DIR to the operator checkout whose lab is up}
OUT_DIR=${OUT_DIR:?set OUT_DIR to a directory for the approver kubeconfig}
LAB_ENVIRONMENT=${LAB_ENVIRONMENT:-$OPERATOR_DIR/demo/.lab/environment}
LAB="$OPERATOR_DIR/demo/bin/lab"
APPROVER=demo-approver
SCHEMA=storefront

say() {
	printf 'demo: %s\n' "$1"
}

fail() {
	printf 'demo: %s\n' "$1" >&2
	exit 1
}

[ -f "$LAB_ENVIRONMENT" ] || fail "no lab environment at $LAB_ENVIRONMENT"
set -a
# shellcheck disable=SC1090 # The lab's NAME=value file.
. "$LAB_ENVIRONMENT"
set +a
NAMESPACE=${E2E_TEST_NAMESPACE:?the lab names no namespace}
CONTROLLER=${E2E_CONTROLLER_NAME:?the lab names no controller}
mkdir -p "$OUT_DIR"

admin() {
	kubectl --kubeconfig "$E2E_KUBECONFIG" "$@"
}

eval "$("$LAB" credentials)"
export PTAH_OCI_USERNAME PTAH_OCI_PASSWORD PTAH_OCI_REGISTRY
PATH="$("$LAB" tools):$PATH"
export PATH
"$LAB" reset

say "the approver: a ServiceAccount bound to the chart's ClusterRole $CONTROLLER-approver in $NAMESPACE"
admin get clusterrole "$CONTROLLER-approver" >/dev/null ||
	fail "the release installed no approver ClusterRole"
admin -n "$NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $APPROVER
  namespace: $NAMESPACE
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: $APPROVER
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: $CONTROLLER-approver
subjects:
  - kind: ServiceAccount
    name: $APPROVER
    namespace: $NAMESPACE
YAML

SERVER=$(admin config view --minify -o jsonpath='{.clusters[0].cluster.server}')
AUTHORITY=$(admin config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
TOKEN=$(admin -n "$NAMESPACE" create token "$APPROVER" --duration=60m)
umask 077
cat >"$OUT_DIR/approver.kubeconfig" <<YAML
apiVersion: v1
kind: Config
clusters:
  - name: ptah-lab
    cluster:
      server: $SERVER
      certificate-authority-data: $AUTHORITY
users:
  - name: $APPROVER
    user:
      token: $TOKEN
contexts:
  - name: ptah-lab
    context:
      cluster: ptah-lab
      user: $APPROVER
      namespace: $NAMESPACE
current-context: ptah-lab
YAML

if kubectl --kubeconfig "$OUT_DIR/approver.kubeconfig" -n "$NAMESPACE" auth can-i create ptahschemas.operator.ptah.run >/dev/null; then
	fail 'the approver may create a schema, so the demo would not show an approver'
fi
kubectl --kubeconfig "$OUT_DIR/approver.kubeconfig" -n "$NAMESPACE" auth can-i create ptahschemaapprovals.operator.ptah.run >/dev/null ||
	fail 'the approver may not approve'

say 'publishing the desired schema as an artifact'
DIGEST=$(ptah schema push "oci://$PTAH_OCI_REGISTRY/schemas/demo:console-$$" \
	--schema-file "$OPERATOR_DIR/demo/schemas/v1.sql" --dialect postgres --plain-http |
	sed -n 's/^Digest: //p')
case "$DIGEST" in
sha256:*) ;;
*) fail 'the push returned no digest' ;;
esac

say "pointing $SCHEMA at $DIGEST with apply: OnApproval"
APPLY=OnApproval INTERVAL=1m "$LAB" manifest "$SCHEMA" "$DIGEST" | admin apply -f - >/dev/null

say 'waiting for the operator to plan and stop for an approval'
admin -n "$NAMESPACE" wait --for=condition=ApprovalRequired --timeout=600s "ptahschema/$SCHEMA" >/dev/null ||
	fail 'the schema did not come to require an approval'
PLAN=$(admin -n "$NAMESPACE" get "ptahschema/$SCHEMA" -o jsonpath='{.status.plan.name}')
admin -n "$NAMESPACE" wait --for=condition=Ready --timeout=120s "ptahschemaplan/$PLAN" >/dev/null ||
	fail "the plan $PLAN did not report its storage ready"

{
	printf 'NAMESPACE=%s\n' "$NAMESPACE"
	printf 'SCHEMA=%s\n' "$SCHEMA"
	printf 'PLAN=%s\n' "$PLAN"
	printf 'APPROVER=system:serviceaccount:%s:%s\n' "$NAMESPACE" "$APPROVER"
} >"$OUT_DIR/facts"
say "ready: $SCHEMA waits for an approval of $PLAN"
