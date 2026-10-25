#!/bin/sh
# Checks, against the cluster and the databases rather than the page, that the
# acceptance rows did what the recording shows:
#
#   - every decision admission stored was made by e2e-approver: the lead's
#     own approval and the author's were refused, and nothing else approved,
#     apart from inventory v2, which the hosted rows approved
#   - storefront applied the plan the approver approved after the change, and
#     the database dropped the column and granted SELECT
#   - billing applied migrations 14 to 17 after their approval
#   - journal's run failed and was not retried behind anyone's back
#   - deliveries' partial run was settled by the approver's acknowledgment
#   - reports' interrupted run was settled the same way, and then applied
#   - catalog and ledger, which the author created through the console's form,
#     have a spec the console's field manager alone wrote and admission's
#     record of the author as their writer; each was approved once, by the
#     approver, and reached its database, and catalog ends resumed and in sync
#   - inventory's read on request was answered: the operator recorded the
#     value the console wrote, under the console's field manager
#   - catalog v2 was approved once, by the username the API server maps the
#     OpenID Connect user's token to, and its database has the new column
#   - inventory v2 was approved once, by the hosted approver's mapped
#     username, and applied after the console was uninstalled
#   - on the second cluster, inventory was approved twice, both times by the
#     author account's mapped username through the agent
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

# deciders prints who admission recorded on each stored decision of a kind,
# apart from the hosted rows' approval of inventory and the OpenID Connect
# row's, which their own parts check.
deciders() {
	ns get "$1" -o json | jq -r --arg field "$2" \
		'[.items[] | select(.spec.schemaRef.name != "inventory") | .spec[$field].username
		  | select(startswith("oidc:") | not)] | unique | join(",")'
}
# E2E_ROWS names the row files that ran, as it does for make e2e-rows, and
# empty means all of them. Each part below checks what one file's rows did, so
# a run of some rows is checked as well as a run of all of them, and a file no
# part checks is refused rather than passed over.
known="acceptance.spec.ts console.spec.ts hosted.spec.ts oidc.spec.ts two-clusters.spec.ts"
for file in ${E2E_ROWS:-}; do
	case " $known " in
	*" $file "*) ;;
	*) fail "nothing here checks the rows of $file" ;;
	esac
done
ran() {
	case " ${E2E_ROWS:-} " in
	"  " | *" $1 "*) return 0 ;;
	*) return 1 ;;
	esac
}
checked=""

if ran acceptance.spec.ts; then
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

	wait_condition ptahmigration reports Ready
	reports=$(ns get ptahmigration reports -o json |
		jq -r '"\(.status.resolvedRun.acknowledgedBy.username // "") \(.status.lastRun.outcome // "")"')
	[ "$reports" = "$APPROVER Applied" ] ||
		fail "reports reads '$reports': not settled by $APPROVER and then applied"
	checked="$checked acceptance.spec.ts"
fi

if ran console.spec.ts; then
	# The resources the console created. A spec another manager touched would
	# mean the console's form was not what made it.
	for resource in ptahschema/catalog ptahmigration/ledger; do
		# kubectl leaves managedFields out of its output unless asked.
		object=$(ns get "$resource" -o json --show-managed-fields)
		writer=$(printf '%s' "$object" | jq -r '.metadata.annotations["operator.ptah.run/last-spec-writer-username"] // ""')
		[ "$writer" = "$AUTHOR" ] || fail "admission recorded ${writer:-nobody} as $resource's writer, not $AUTHOR"
		managers=$(printf '%s' "$object" | jq -r '[.metadata.managedFields // error("no managedFields") | .[] | select(.fieldsV1["f:spec"] != null) | .manager] | unique | join(",")')
		[ "$managers" = ptah-console ] || fail "$resource's spec was written by '$managers', not by the console alone"
	done
	wait_condition ptahschema catalog InSync
	[ "$(ns get ptahschema catalog -o jsonpath='{.spec.suspend}')" != true ] || fail 'catalog is still suspended'
	# The OpenID Connect row approves catalog v2 later; its part checks that.
	catalog=$(ns get ptahschemaapprovals -o json |
		jq -r '[.items[] | select(.spec.schemaRef.name == "catalog") | .spec.approver.username
		  | select(startswith("oidc:") | not)] | join(",")')
	[ "$catalog" = "$APPROVER" ] || fail "catalog's approvals read '$catalog', not one by $APPROVER"
	products=$(postgres -d catalog -c \
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'products'")
	[ "$products" = 1 ] || fail 'catalog has no products table'
	wait_condition ptahmigration ledger Ready
	items=$(ns get ptahmigration ledger -o json | jq -r '[.spec.artifact.configMap.items[].path] | sort | join(",")')
	[ "$items" = 0000000001_create_ledger_entries.down.sql,0000000001_create_ledger_entries.up.sql ] ||
		fail "ledger reads '$items' from its ConfigMap, not the up and down files the row named"
	ledger=$(ns get ptahmigrationapprovals -o json |
		jq -r '[.items[] | select(.spec.migrationRef.name == "ledger") | .spec.approver.username] | join(",")')
	[ "$ledger" = "$APPROVER" ] || fail "ledger's approvals read '$ledger', not one by $APPROVER"
	entries=$(postgres -d ledger -c \
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'ledger_entries'")
	[ "$entries" = 1 ] || fail 'ledger has no ledger_entries table'

	# The read the author asked of inventory: the operator answered exactly the
	# value the console wrote, and the console's field manager owns that
	# annotation and nothing else it set there.
	read=$(ns get ptahschema inventory -o json --show-managed-fields | jq -r '
		(.metadata.annotations["operator.ptah.run/read-requested-at"] // "") as $asked
		| [.metadata.managedFields // error("no managedFields") | .[] | select(.manager == "ptah-console") | .fieldsV1
			| paths(type == "object" and length == 0) | select(last != ".") | join(".")] as $owned
		| if $asked == "" then "no request"
		elif (.status.lastReadRequest // "") != $asked then "unanswered: \($asked)"
		elif $owned != ["f:metadata.f:annotations.f:operator.ptah.run/read-requested-at"] then "the console owns \($owned)"
		else "answered" end')
	[ "$read" = answered ] || fail "inventory's read on request: $read"
	checked="$checked console.spec.ts"
fi

if ran hosted.spec.ts; then
	# The hosted rows: admission recorded the account's mapped username, because
	# the console impersonated it, and the operator applied the plan after the
	# console was gone.
	hosted=$(ns get ptahschemaapprovals -o json | jq -r \
		'[.items[] | select(.spec.schemaRef.name == "inventory") | "\(.spec.approver.username) \(.spec.planRef.name)"] | join(",")')
	[ "$hosted" = "$HOSTED_APPROVER $HOSTED_INVENTORY_PLAN" ] ||
		fail "inventory's approvals read '$hosted', not one by $HOSTED_APPROVER for $HOSTED_INVENTORY_PLAN"
	wait_condition ptahschema inventory InSync
	location=$(postgres -d inventory -c \
		"SELECT count(*) FROM information_schema.columns WHERE table_name = 'items' AND column_name = 'location'")
	[ "$location" = 1 ] || fail 'inventory has no items.location column'
	# The two-cluster rows install the console again; without them it stays
	# uninstalled.
	if [ -z "${HOSTED_ADMIN:-}" ]; then
		[ -z "$(admin -n ptah-console get deployments -o name)" ] || fail 'the hosted console is still installed'
	fi
	checked="$checked hosted.spec.ts"
fi

if ran oidc.spec.ts; then
	# The OpenID Connect row: admission recorded the username the API server
	# mapped the person's ID token to, once, for catalog v2, after the
	# approver's approval of the plan the author's form produced, and the
	# database has the column v2 adds.
	oidc=$(ns get ptahschemaapprovals -o json |
		jq -r '[.items[] | select(.spec.schemaRef.name == "catalog")] | sort_by(.metadata.creationTimestamp)
		  | [.[].spec.approver.username] | join(",")')
	[ "$oidc" = "$APPROVER,$OIDC_USERNAME" ] ||
		fail "catalog's approvals read '$oidc', not one by $APPROVER and then one by $OIDC_USERNAME"
	wait_condition ptahschema catalog InSync
	sku=$(postgres -d catalog -c \
		"SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'products' AND column_name = 'sku'")
	[ "$sku" = 1 ] || fail 'catalog v2, which the OpenID Connect user approved, did not add products.sku'
	checked="$checked oidc.spec.ts"
fi

if ran two-clusters.spec.ts; then
	# The two-cluster rows: lab-2's own admission recorded every approval there
	# as the author account's username, once for each of inventory's two plans,
	# and the console's removal touched none of it.
	[ -n "${TARGET_CONTEXT:-}" ] || fail 'the two-cluster rows ran, and no second cluster is named'
	target=$("$E2E_DIR/external.sh" approvers inventory)
	[ "$target" = "$HOSTED_AUTHOR,$HOSTED_AUTHOR" ] ||
		fail "lab-2's inventory approvals read '$target', not two by $HOSTED_AUTHOR"
	"$E2E_DIR/external.sh" insync inventory
	say "verified: lab-2 approved inventory twice, as $HOSTED_AUTHOR, and kept both after the connection was removed"
	checked="$checked two-clusters.spec.ts"
fi

[ -n "$checked" ] || fail 'no rows were checked'
say "verified the rows of ${checked# } against the cluster: each decision was the one the recording shows, and each did what it shows"
