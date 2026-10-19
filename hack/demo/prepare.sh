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
AUTHOR=demo-author
VIEWER=demo-viewer
OUTSIDER=demo-outsider
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

say "the author: a ServiceAccount that may edit the schema and read its plans, and may not approve"
admin -n "$NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $AUTHOR
  namespace: $NAMESPACE
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: $AUTHOR
  namespace: $NAMESPACE
rules:
  - apiGroups: [operator.ptah.run]
    resources: [ptahschemas]
    verbs: [get, list, watch, create, update, patch]
  - apiGroups: [operator.ptah.run]
    resources: [ptahschemaplans, ptahschemaplanchunks, ptahschemaapprovals]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: $AUTHOR
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: $AUTHOR
subjects:
  - kind: ServiceAccount
    name: $AUTHOR
    namespace: $NAMESPACE
YAML

say "the viewer: a ServiceAccount that may read schemas and plans, and not the chunks a plan's SQL is stored in"
admin -n "$NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $VIEWER
  namespace: $NAMESPACE
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: $VIEWER
  namespace: $NAMESPACE
rules:
  - apiGroups: [operator.ptah.run]
    resources: [ptahschemas, ptahschemaplans, ptahschemaapprovals]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: $VIEWER
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: $VIEWER
subjects:
  - kind: ServiceAccount
    name: $VIEWER
    namespace: $NAMESPACE
YAML

# The outsider is a ServiceAccount with no grant at all in the namespace.
say "the outsider: a ServiceAccount with no grant in $NAMESPACE"
admin -n "$NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: $OUTSIDER
  namespace: $NAMESPACE
YAML

SERVER=$(admin config view --minify -o jsonpath='{.clusters[0].cluster.server}')
AUTHORITY=$(admin config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
umask 077

# write_kubeconfig writes a token kubeconfig for one ServiceAccount: what a
# person would have, and nothing the lab's own administrator holds.
write_kubeconfig() {
	account=$1
	file=$2
	token=$(admin -n "$NAMESPACE" create token "$account" --duration=90m)
	cat >"$OUT_DIR/$file" <<YAML
apiVersion: v1
kind: Config
clusters:
  - name: ptah-lab
    cluster:
      server: $SERVER
      certificate-authority-data: $AUTHORITY
users:
  - name: $account
    user:
      token: $token
contexts:
  - name: ptah-lab
    context:
      cluster: ptah-lab
      user: $account
      namespace: $NAMESPACE
current-context: ptah-lab
YAML
}
write_kubeconfig "$APPROVER" approver.kubeconfig
write_kubeconfig "$AUTHOR" author.kubeconfig
write_kubeconfig "$VIEWER" viewer.kubeconfig
write_kubeconfig "$OUTSIDER" outsider.kubeconfig

if kubectl --kubeconfig "$OUT_DIR/approver.kubeconfig" -n "$NAMESPACE" auth can-i create ptahschemas.operator.ptah.run >/dev/null; then
	fail 'the approver may create a schema, so the demo would not show an approver'
fi
kubectl --kubeconfig "$OUT_DIR/approver.kubeconfig" -n "$NAMESPACE" auth can-i create ptahschemaapprovals.operator.ptah.run >/dev/null ||
	fail 'the approver may not approve'
if kubectl --kubeconfig "$OUT_DIR/author.kubeconfig" -n "$NAMESPACE" auth can-i create ptahschemaapprovals.operator.ptah.run >/dev/null; then
	fail 'the author may approve, so the refusal row would prove nothing'
fi
if kubectl --kubeconfig "$OUT_DIR/viewer.kubeconfig" -n "$NAMESPACE" auth can-i get ptahschemaplanchunks.operator.ptah.run >/dev/null; then
	fail 'the viewer may read chunks, so the not-granted row would prove nothing'
fi
if kubectl --kubeconfig "$OUT_DIR/outsider.kubeconfig" -n "$NAMESPACE" auth can-i list ptahschemas.operator.ptah.run >/dev/null; then
	fail 'the outsider may list schemas, so the no-access row would prove nothing'
fi

say 'publishing the desired schema as an artifact'
DIGEST=$(ptah schema push "oci://$PTAH_OCI_REGISTRY/schemas/demo:console-$$" \
	--schema-file "$OPERATOR_DIR/demo/schemas/v1.sql" --dialect postgres --plain-http |
	sed -n 's/^Digest: //p')
case "$DIGEST" in
sha256:*) ;;
*) fail 'the push returned no digest' ;;
esac

# Each interval the operator refreshes the source and plans again, and the
# plan is not there to approve while it does. Ten minutes keeps the plan the
# recording reviews published for the whole recording; the proof after an
# Apply is scheduled from the Job, not from the interval.
say "pointing $SCHEMA at $DIGEST with apply: OnApproval"
APPLY=OnApproval INTERVAL=10m "$LAB" manifest "$SCHEMA" "$DIGEST" | admin apply -f - >/dev/null

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
	printf 'AUTHOR=system:serviceaccount:%s:%s\n' "$NAMESPACE" "$AUTHOR"
	printf 'VIEWER=system:serviceaccount:%s:%s\n' "$NAMESPACE" "$VIEWER"
	printf 'OUTSIDER=system:serviceaccount:%s:%s\n' "$NAMESPACE" "$OUTSIDER"
} >"$OUT_DIR/facts"
say "ready: $SCHEMA waits for an approval of $PLAN"
