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
#   act.sh run reports                apply the reports migrations
#   act.sh interrupt reports          delete the Apply Job while it runs, and
#                                     wait for the run to be recorded unresolved
#   act.sh wait-run <name> <outcome>  wait for a migration's last run to
#                                     end with an outcome
#   act.sh repair deliveries|reports  undo what an unsettled run left
#   act.sh outage start|stop          freeze or release the API server's
#                                     load balancer
#   act.sh hosted ...                 the hosted console; see hosted.sh
#   act.sh external ...               the second cluster; see external.sh
#   act.sh insync <schema>            wait for a schema to be InSync
#   act.sh ready <migration>          wait for a migration to be Ready
#   act.sh next-read <schema>         print the read the interval schedules
#                                     next, at least three minutes off
#   act.sh read-done <schema> <next>  wait for a read on request to finish,
#                                     sooner than the scheduled <next>
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab

[ "$#" -ge 1 ] || fail 'usage: act.sh <action> [arguments]'
action=$1
shift
case "$action" in
hosted)
	exec "$E2E_DIR/hosted.sh" "$@"
	;;
external)
	exec "$E2E_DIR/external.sh" "$@"
	;;
insync)
	wait_condition ptahschema "${1:?name a schema}" InSync 900s
	;;
ready)
	wait_condition ptahmigration "${1:?name a migration}" Ready 900s
	;;
next-read)
	# A row that asks for a read has to tell its answer from the one the
	# interval starts anyway, so the scheduled read it prints is far enough
	# off for the requested one to finish first. A nearer one, or one that
	# is running, is waited out. A read is a chain of operations with gaps
	# between them, and the operator reschedules the next read as the chain
	# begins, so a read has ended only when the schema rests in a phase the
	# operator starts a requested read from.
	schema=${1:?name a schema}
	deadline=$(($(date +%s) + 900))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		next=$(admin -n "$NAMESPACE" get "ptahschema/$schema" -o json | jq -r '
			if (.status.activeOperation // null) != null or (.status.nextReconciliationTime // "") == "" then ""
			elif ([.status.phase] | inside(["InSync", "Blocked", "ReadyToApply", "AwaitingApproval"]) | not) then ""
			elif ((.status.nextReconciliationTime | sub("\\.[0-9]+"; "") | fromdateiso8601) - now) < 180 then ""
			else .status.nextReconciliationTime end')
		[ -n "$next" ] && {
			printf '%s\n' "$next"
			exit 0
		}
		sleep 5
	done
	fail "ptahschema/$schema never had a read scheduled three minutes off with nothing running"
	;;
read-done)
	# The read the console asked for counts once the operator answered that
	# request by starting a read, said so in a ReadRequested Event, and then
	# completed a cycle, before the read the interval had scheduled: a cycle
	# at or after that one proves nothing about the request. A request the
	# operator answers while a read already runs starts none and has no Event.
	schema=${1:?name a schema}
	scheduled=${2:?give the read that was scheduled before the request}
	deadline=$(($(date +%s) + 600))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		verdict=$(admin -n "$NAMESPACE" get "ptahschema/$schema" -o json | jq -r --arg scheduled "$scheduled" '
			def seconds: sub("\\.[0-9]+"; "") | fromdateiso8601;
			(.metadata.annotations["operator.ptah.run/read-requested-at"] // "") as $asked
			| (.status.lastSuccessfulReconciliation // "") as $done
			| if $asked == "" then "unasked"
			elif (.status.lastReadRequest // "") != $asked then "waiting"
			elif $done == "" or ($done | seconds) <= ($asked | seconds) then "waiting"
			elif ($done | seconds) >= ($scheduled | seconds) then "late \($done)"
			else "done \($asked)" end')
		case "$verdict" in
		done*)
			asked=${verdict#done }
			started=$(admin -n "$NAMESPACE" get events --field-selector "involvedObject.name=$schema,reason=ReadRequested" -o json |
				jq -r --arg asked "$asked" '
					def seconds: sub("\\.[0-9]+"; "") | fromdateiso8601;
					[.items[] | (.eventTime // .lastTimestamp // .firstTimestamp // "") | select(. != "") | seconds
						| select(. >= ($asked | seconds))] | length')
			[ "$started" -gt 0 ] || fail "ptahschema/$schema answered the request at $asked without starting a read: no ReadRequested Event"
			exit 0
			;;
		unasked) fail "nothing asked ptahschema/$schema to read" ;;
		late*) fail "ptahschema/$schema completed a cycle at ${verdict#late }, not before the scheduled $scheduled" ;;
		esac
		sleep 3
	done
	fail "ptahschema/$schema never completed the read it was asked for"
	;;
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
run)
	[ "${1:-}" = reports ] || fail 'only reports is run'
	APPLY=Always INTERVAL=10m render reports "$(digest reports-release-1)" | admin apply -f - >/dev/null
	;;
interrupt)
	[ "${1:-}" = reports ] || fail 'only reports is interrupted'
	# The operator's own unresolved-run row does the same: wait until the
	# Apply is really running the migration that sleeps, then delete its
	# Job. Nothing then says what the run did.
	deadline=$(($(date +%s) + 600))
	job=""
	while [ "$(date +%s)" -lt "$deadline" ]; do
		job=$(admin -n "$NAMESPACE" get ptahmigration/reports -o jsonpath='{.status.activeOperation.jobName}')
		sleeping=$(postgres -d reports -c "SELECT count(*) FROM pg_stat_activity WHERE datname = 'reports' AND query LIKE '%pg_sleep(60)%' AND pid <> pg_backend_pid()")
		[ -n "$job" ] && [ "$sleeping" = 1 ] && break
		sleep 2
	done
	[ -n "$job" ] && [ "${sleeping:-0}" = 1 ] || fail 'the reports Apply never reached the migration that sleeps'
	admin -n "$NAMESPACE" delete job "$job" --cascade=background --wait=false >/dev/null
	while [ "$(date +%s)" -lt "$deadline" ]; do
		outcome=$(admin -n "$NAMESPACE" get ptahmigration/reports -o jsonpath='{.status.unresolvedRun.outcome}')
		[ -n "$outcome" ] && exit 0
		sleep 2
	done
	fail 'the interrupted reports run was never recorded as unresolved'
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
	case "${1:-}" in
	deliveries)
		# What the partial run committed, undone the way the operator's own
		# scenario undoes it: the column it added, and its row in the
		# revision table. A real database needs its changes judged first.
		mysql_demo "ALTER TABLE deliveries DROP COLUMN status; DELETE FROM schema_migrations WHERE version = 2"
		;;
	reports)
		# The interrupted migration only slept, so once no session of it is
		# left, its unfinished revision is all there is to remove.
		deadline=$(($(date +%s) + 300))
		while [ "$(date +%s)" -lt "$deadline" ]; do
			left=$(postgres -d reports -c "SELECT count(*) FROM pg_stat_activity WHERE datname = 'reports' AND query LIKE '%pg_sleep(60)%' AND pid <> pg_backend_pid()")
			[ "$left" = 0 ] && break
			sleep 2
		done
		[ "${left:-1}" = 0 ] || fail 'the interrupted session never ended'
		postgres -d reports -c "DELETE FROM schema_migrations WHERE version = 2 AND state <> 'applied'" >/dev/null
		;;
	*) fail 'repair deliveries or reports' ;;
	esac
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
