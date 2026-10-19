#!/bin/sh
# Checks, against the cluster and not the page, that the demo's approval did
# what the video shows: admission recorded the approver, the operator
# applied the plan, and the database has the table.
#
#   OPERATOR_DIR   the operator checkout whose lab is up
#   OUT_DIR        the directory prepare.sh wrote
set -eu

OPERATOR_DIR=${OPERATOR_DIR:?set OPERATOR_DIR}
OUT_DIR=${OUT_DIR:?set OUT_DIR}
LAB_ENVIRONMENT=${LAB_ENVIRONMENT:-$OPERATOR_DIR/demo/.lab/environment}

fail() {
	printf 'verify: %s\n' "$1" >&2
	exit 1
}

set -a
# shellcheck disable=SC1090 # The lab's NAME=value file.
. "$LAB_ENVIRONMENT"
# shellcheck disable=SC1091 # Written by prepare.sh.
. "$OUT_DIR/facts"
set +a

admin() {
	kubectl --kubeconfig "$E2E_KUBECONFIG" -n "$NAMESPACE" "$@"
}

admin wait --for=condition=InSync --timeout=600s "ptahschema/$SCHEMA" >/dev/null ||
	fail "$SCHEMA did not reach InSync"
REASON=$(admin get "ptahschema/$SCHEMA" -o jsonpath='{.status.conditions[?(@.type=="InSync")].reason}')
[ "$REASON" = ScopedConverged ] || fail "InSync carries $REASON"

APPLIED=$(admin get "ptahschema/$SCHEMA" -o jsonpath='{.status.applied.planName}')
[ "$APPLIED" = "$PLAN" ] || fail "the schema applied $APPLIED, not the approved $PLAN"

RECORDED=$(admin get ptahschemaapprovals -o json |
	jq -r --arg plan "$PLAN" '[.items[] | select(.spec.planRef.name == $plan) | .spec.approver.username] | unique | join(",")')
[ "$RECORDED" = "${APPROVER:?}" ] || fail "the approval of $PLAN records '$RECORDED', not $APPROVER"

admin exec deploy/demo-psql -- psql -c '\d customers' | grep -q email ||
	fail 'the customers table has no email column'

printf 'verify: %s applied %s, approved by %s through the console\n' "$SCHEMA" "$PLAN" "$APPROVER"
