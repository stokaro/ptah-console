#!/bin/sh
# What happens in the cluster while a row is on screen. The Playwright rows
# call this as the cluster's administrator, never as the person signed in to
# the console: it stands for another team publishing, a run failing, or the
# API server going away.
#
#   act.sh publish storefront v4|v5   point storefront at another version
#   act.sh wait-plan <kind> <name> [<other plan>]
#                                     wait for a plan to approve, print it
#   act.sh plan <kind> <name>         print the plan a resource waits on
#   act.sh applied storefront         print the plan storefront last applied
#   act.sh fail journal               apply the journal migrations that fail
#   act.sh wait-run <name> <outcome>  wait for a migration's last run to
#                                     end with an outcome
#   act.sh repair deliveries          undo what the partial run committed
#   act.sh outage start|stop          freeze or release the API server's
#                                     load balancer
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab

[ "$#" -ge 1 ] || fail 'usage: act.sh <action> [arguments]'
action=$1
shift
case "$action" in
publish)
	[ "${1:-}" = storefront ] || fail 'only storefront is published again'
	case "${2:-}" in
	v4 | v5) ;;
	*) fail 'publish storefront v4 or v5' ;;
	esac
	APPLY=OnApproval ALLOW_DESTRUCTIVE=true INTERVAL=10m \
		"$LAB" manifest storefront "$(digest "storefront-$2")" | admin apply -f - >/dev/null
	;;
wait-plan)
	wait_plan "$1" "$2" "${3:-}"
	;;
plan)
	plan_of "$1" "$2"
	;;
applied)
	admin -n "$NAMESPACE" get "ptahschema/$1" -o jsonpath='{.status.applied.planRef.name}'
	;;
fail)
	[ "${1:-}" = journal ] || fail 'only journal fails'
	APPLY=Always INTERVAL=10m render journal "$(digest journal-release-1)" | admin apply -f - >/dev/null
	;;
wait-run)
	deadline=$(($(date +%s) + 600))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		outcome=$(admin -n "$NAMESPACE" get "ptahmigration/$1" -o jsonpath='{.status.lastRun.outcome}')
		[ "$outcome" = "$2" ] && exit 0
		sleep 3
	done
	admin -n "$NAMESPACE" get "ptahmigration/$1" -o json |
		jq -r '(.status.conditions[]? | "\(.type)=\(.status) \(.reason): \(.message)"), "lastRun \(.status.lastRun.outcome // "-")"' >&2
	fail "ptahmigration/$1's last run never ended $2"
	;;
repair)
	[ "${1:-}" = deliveries ] || fail 'only deliveries is repaired'
	# What the partial run committed, undone the way the operator's own
	# scenario undoes it: the column it added, and its row in the revision
	# table. A real database needs its changes judged first.
	mysql_demo "ALTER TABLE deliveries DROP COLUMN status; DELETE FROM schema_migrations WHERE version = 2"
	;;
outage)
	balancer="${E2E_KIND_CLUSTER_NAME:?the lab names no cluster}-external-load-balancer"
	case "${1:-}" in
	start) verb=pause ;;
	stop) verb=unpause ;;
	*) fail 'outage start or stop' ;;
	esac
	# Pausing the balancer freezes the address every kubeconfig names: a
	# connection stays open and nothing answers on it, which is what a
	# client sees of an API server that stopped responding. Pods reach the
	# API servers through the kubernetes Service, so the operator keeps
	# running; the worker's kubelet loses the API until the balancer is
	# released, so a row keeps the outage short.
	DOCKER_CONFIG="${E2E_DOCKER_CONFIG:-${DOCKER_CONFIG:-}}" \
		docker --context "${E2E_DOCKER_CONTEXT:?the lab names no Docker context}" "$verb" "$balancer" >/dev/null
	;;
*)
	fail "no action $action"
	;;
esac
