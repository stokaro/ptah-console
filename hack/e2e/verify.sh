#!/bin/sh
# Checks, against the cluster and the databases rather than the page, that the
# acceptance rows did what the recording shows:
#
#   - every decision admission stored was made by e2e-approver: the lead's
#     own approval and the author's were refused, and nothing else approved
#   - storefront applied the plan the approver approved after the change, and
#     the database dropped the column and granted SELECT
#   - billing applied migrations 14 to 17 after their approval
#   - journal's run failed and was not retried behind anyone's back
#   - deliveries' partial run was settled by the approver's acknowledgment
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab
set -a
# shellcheck disable=SC1091 # Written by personas.sh and fixtures.sh.
. "$OUT_DIR/facts"
set +a

ns() {
	admin -n "$NAMESPACE" "$@"
}

# deciders prints who admission recorded on each stored decision of a kind.
deciders() {
	ns get "$1" -o json | jq -r --arg field "$2" '[.items[] | .spec[$field].username] | unique | join(",")'
}
for kind in ptahschemaapprovals:approver ptahmigrationapprovals:approver ptahmigrationrunacknowledgments:acknowledgedBy; do
	who=$(deciders "${kind%%:*}" "${kind##*:}")
	[ "$who" = "$APPROVER" ] || fail "${kind%%:*} were admitted for '${who}', not only for $APPROVER"
done

wait_condition ptahschema storefront InSync
reason=$(ns get ptahschema storefront -o jsonpath='{.status.conditions[?(@.type=="InSync")].reason}')
[ "$reason" = ScopedConverged ] || fail "storefront is InSync for $reason"
applied=$(ns get ptahschema storefront -o jsonpath='{.status.applied.planRef.name}')
# Two approvals: the destructive plan, and the plan that replaced the one the
# approver was reading. The replaced plan was never approved, so a third
# approval is the console sending a decision it reported as not sent.
approved=$(ns get ptahschemaapprovals -o json |
	jq -r '[.items[] | select(.spec.schemaRef.name == "storefront") | .spec.planRef.name] | sort | join(",")')
case "$approved" in
*,*,*) fail "storefront has more than two approvals: $approved" ;;
*,*) ;;
*) fail "storefront has fewer than two approvals: '$approved'" ;;
esac
case ",$approved," in
*,"$applied",*) ;;
*) fail "storefront applied $applied, which none of its approvals names: $approved" ;;
esac
columns=$(ns exec deploy/demo-psql -- psql -qAt -c \
	"SELECT string_agg(table_name || '.' || column_name, ',' ORDER BY table_name, column_name) FROM information_schema.columns WHERE table_schema = 'public' AND table_name IN ('customers', 'orders')")
case ",$columns," in
*,customers.email,*) fail 'customers still has its email column' ;;
esac
for column in orders.placed_at orders.note; do
	case ",$columns," in
	*,"$column",*) ;;
	*) fail "the database has no $column: $columns" ;;
	esac
done
granted=$(ns exec deploy/demo-psql -- psql -qAt -c "SELECT has_table_privilege('public', 'orders', 'SELECT')")
[ "$granted" = t ] || fail 'orders is not readable by PUBLIC'

wait_condition ptahmigration billing Ready
billing=$(ns get ptahmigrationapprovals -o json |
	jq '[.items[] | select(.spec.migrationRef.name == "billing")] | length')
[ "$billing" = 1 ] || fail "billing has $billing approvals, not one"
tables=$(postgres -d billing -c \
	"SELECT string_agg(tablename, ',' ORDER BY tablename) FROM pg_tables WHERE schemaname = 'public' AND tablename IN ('subscriptions', 'usage_records')")
[ "$tables" = subscriptions,usage_records ] || fail "billing has '$tables' of migrations 14 to 17"

outcome=$(ns get ptahmigration journal -o jsonpath='{.status.lastRun.outcome}')
[ "$outcome" = Failed ] || fail "journal's last run is '$outcome', not Failed"

resolved=$(ns get ptahmigration deliveries -o json | jq -c '.status.resolvedRun // {}')
[ "$(printf '%s' "$resolved" | jq -r '.acknowledgedBy.username // ""')" = "$APPROVER" ] ||
	fail "deliveries' run was not settled by $APPROVER: $resolved"
acknowledgment=$(printf '%s' "$resolved" | jq -r '.acknowledgmentRef.name // ""')
[ -n "$acknowledgment" ] || fail 'deliveries names no acknowledgment'
consumed=$(ns get ptahmigrationrunacknowledgment "$acknowledgment" \
	-o jsonpath='{.status.conditions[?(@.type=="Consumed")].status}')
[ "$consumed" = True ] || fail "the acknowledgment $acknowledgment is not consumed"
deliveries=$(ns get ptahmigrationapprovals -o json |
	jq '[.items[] | select(.spec.migrationRef.name == "deliveries")] | length')
[ "$deliveries" = 1 ] || fail "deliveries has $deliveries approvals, not one"

say "verified: every decision was $APPROVER's, and each one did what the rows show"
