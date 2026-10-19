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

# The plan the approver approved is the one the approval names, read from the
# approval admission stamped, not from what the recording saw.
APPROVED=$(admin get ptahschemaapprovals -o json |
	jq -r --arg who "${APPROVER:?}" '[.items[] | select(.spec.approver.username == $who) | .spec.planRef.name] | unique | join(",")')
[ -n "$APPROVED" ] || fail "no approval records $APPROVER"
case "$APPROVED" in
*,*) fail "$APPROVER approved more than one plan: $APPROVED" ;;
esac
APPLIED=$(admin get "ptahschema/$SCHEMA" -o jsonpath='{.status.applied.planName}')
[ "$APPLIED" = "$APPROVED" ] || fail "the schema applied $APPLIED, not the approved $APPROVED"

OTHERS=$(admin get ptahschemaapprovals -o json |
	jq -r --arg who "$APPROVER" '[.items[] | select(.spec.approver.username != $who) | .spec.approver.username] | unique | join(",")')
[ -z "$OTHERS" ] || fail "approvals were admitted for $OTHERS as well"

admin exec deploy/demo-psql -- psql -c '\d customers' | grep -q email ||
	fail 'the customers table has no email column'

printf 'verify: %s applied %s, approved by %s through the console\n' "$SCHEMA" "$APPROVED" "$APPROVER"
