#!/bin/sh
# Gives each person in the acceptance rows a kubeconfig of their own: a client
# certificate the cluster issued through the CSR API, for a user name and the
# groups the roles below are bound to. Nothing here is a ServiceAccount, and
# nothing is a copy of the administrator's credentials.
#
# The roles are the operator's own examples, rendered for the lab namespace,
# except the viewer's, which the operator does not ship:
#
#   viewer    reads every Ptah kind except the chunks a plan's SQL is in
#   reviewer  approver-plan-reader-role: get schemas, plans and chunks
#   author    desired-state-author-role, and the plan reader
#   lead      what the author has, and the chart's approver ClusterRole
#   approver  the chart's approver ClusterRole
#   diag      diagnostic-reader-role: Ptah kinds, Jobs, Pods, logs, Events
#   outsider  nothing in the namespace
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab

VIEWERS=e2e:viewers
REVIEWERS=e2e:plan-reviewers
AUTHORS=e2e:desired-state-authors
APPROVERS=e2e:approvers
DIAGNOSTICS=e2e:diagnostic-readers

# example applies one of the operator's example roles to the lab namespace,
# bound to one group.
example() {
	example_file="$OPERATOR_DIR/examples/$1.yaml"
	[ -f "$example_file" ] || fail "the operator ships no $example_file"
	sed -e "s|namespace: application|namespace: $NAMESPACE|" -e "s|$2|$3|" "$example_file" |
		admin apply -f - >/dev/null
}

say "binding the operator's example roles in $NAMESPACE"
example approver-plan-reader-role '<plan-reviewer-group>' "$REVIEWERS"
example desired-state-author-role '<desired-state-author-group>' "$AUTHORS"
example diagnostic-reader-role '<diagnostic-reader-group>' "$DIAGNOSTICS"
admin get clusterrole "$CONTROLLER-approver" >/dev/null ||
	fail "the release installed no approver ClusterRole"
admin -n "$NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ptah-console-e2e-approvers
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: $CONTROLLER-approver
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: Group
    name: $APPROVERS
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: ptah-console-e2e-viewer
  namespace: $NAMESPACE
rules:
  - apiGroups: [operator.ptah.run]
    resources:
      - ptahschemas
      - ptahschemaplans
      - ptahschemaapprovals
      - ptahmigrations
      - ptahmigrationplans
      - ptahmigrationapprovals
      - ptahmigrationrunacknowledgments
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ptah-console-e2e-viewers
  namespace: $NAMESPACE
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: ptah-console-e2e-viewer
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: Group
    name: $VIEWERS
YAML

CLUSTER=$(admin config view --raw --minify -o json | jq -ce '.clusters[0].cluster') ||
	fail "the administrator's kubeconfig names no cluster"
umask 077

# issue asks the cluster for a client certificate for one user and its
# groups, approves it as the administrator, and writes a kubeconfig that holds
# the certificate and the key that never left this machine.
issue() {
	issue_who=$1
	shift
	issue_subject="/CN=e2e-$issue_who"
	for issue_group in "$@"; do
		issue_subject="$issue_subject/O=$issue_group"
	done
	issue_key="$OUT_DIR/$issue_who.key"
	openssl ecparam -name prime256v1 -genkey -noout -out "$issue_key" 2>/dev/null
	openssl req -new -key "$issue_key" -subj "$issue_subject" -out "$OUT_DIR/$issue_who.csr" 2>/dev/null
	issue_name="ptah-console-e2e-$issue_who"
	admin delete csr "$issue_name" --ignore-not-found >/dev/null
	jq -n --arg name "$issue_name" --arg request "$(base64 <"$OUT_DIR/$issue_who.csr" | tr -d '\n')" '{
      apiVersion: "certificates.k8s.io/v1", kind: "CertificateSigningRequest",
      metadata: {name: $name},
      spec: {
        request: $request, signerName: "kubernetes.io/kube-apiserver-client",
        expirationSeconds: 21600, usages: ["client auth"]
      }
    }' | admin create -f - >/dev/null
	admin certificate approve "$issue_name" >/dev/null
	issue_certificate=""
	for _ in $(seq 1 60); do
		issue_certificate=$(admin get csr "$issue_name" -o jsonpath='{.status.certificate}')
		[ -n "$issue_certificate" ] && break
		sleep 1
	done
	[ -n "$issue_certificate" ] || fail "the cluster issued no certificate for e2e-$issue_who"
	admin delete csr "$issue_name" >/dev/null
	jq -n \
		--argjson cluster "$CLUSTER" \
		--arg user "e2e-$issue_who" \
		--arg certificate "$issue_certificate" \
		--arg key "$(base64 <"$issue_key" | tr -d '\n')" \
		--arg namespace "$NAMESPACE" '{
      apiVersion: "v1", kind: "Config",
      clusters: [{name: "ptah-lab", cluster: $cluster}],
      users: [{name: $user, user: {"client-certificate-data": $certificate, "client-key-data": $key}}],
      contexts: [{name: "ptah-lab", context: {cluster: "ptah-lab", user: $user, namespace: $namespace}}],
      "current-context": "ptah-lab"
    }' >"$OUT_DIR/$issue_who.kubeconfig"
	rm -f "$OUT_DIR/$issue_who.csr" "$issue_key"
	fact "$(printf '%s' "$issue_who" | tr '[:lower:]' '[:upper:]')" "e2e-$issue_who"
}

say 'issuing a client certificate for each person'
issue viewer "$VIEWERS"
issue reviewer "$REVIEWERS"
issue author "$AUTHORS" "$REVIEWERS"
issue lead "$AUTHORS" "$REVIEWERS" "$APPROVERS"
issue approver "$APPROVERS"
issue diag "$DIAGNOSTICS"
issue outsider e2e:outsiders
fact NAMESPACE "$NAMESPACE"

# can says whether a persona may do something, by the API server's answer.
can() {
	persona "$1" -n "$NAMESPACE" auth can-i "$2" "$3" >/dev/null 2>&1
}

# A row proves a refusal only if the identity it signs in as is refused, and
# a grant only if it is granted. Each line below is the premise of a row.
say 'checking each identity has exactly the grants its rows assume'
check() {
	if can "$2" "$3" "$4"; then
		[ "$1" = may ] || fail "e2e-$2 may $3 $4, and its rows assume it may not"
	else
		[ "$1" = "may-not" ] || fail "e2e-$2 may not $3 $4, and its rows assume it may"
	fi
}
check may viewer list ptahschemas.operator.ptah.run
check may-not viewer get ptahschemaplanchunks.operator.ptah.run
check may reviewer get ptahschemaplanchunks.operator.ptah.run
check may-not reviewer list ptahschemas.operator.ptah.run
check may author patch ptahschemas.operator.ptah.run
check may-not author create ptahschemaapprovals.operator.ptah.run
check may lead patch ptahschemas.operator.ptah.run
check may lead create ptahschemaapprovals.operator.ptah.run
check may approver create ptahschemaapprovals.operator.ptah.run
check may approver create ptahmigrationrunacknowledgments.operator.ptah.run
check may-not approver patch ptahschemas.operator.ptah.run
check may diag get pods/log
check may-not diag create ptahmigrationrunacknowledgments.operator.ptah.run
check may-not outsider list ptahschemas.operator.ptah.run

# The certificate, not a token, is what authenticates each of them.
who=$(persona approver auth whoami -o jsonpath='{.status.userInfo.username}')
[ "$who" = e2e-approver ] || fail "the approver's kubeconfig authenticates as $who"
say "personas ready in $OUT_DIR"
