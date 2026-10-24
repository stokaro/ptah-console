#!/bin/sh
# Puts the lab `make e2e-up` built into the states the early-adopter tasks in
# docs/usability/early-adopters.md start from. It acts as the lab's
# administrator, and approves deliveries as the approver persona, the way
# another team and an incident would; a participant sees only the result.
#
#   task 1  storefront v3 waits for an approval (fixtures.sh leaves it so)
#   task 2  journal's second migration failed
#   task 3  deliveries' release 2 stopped part-way on MySQL
#   task 4  reports' Apply Job was deleted while it ran
#   task 5  payroll, in a namespace no persona may read (fixtures.sh)
#
# Run it once per session, on a lab nobody else is using: it changes what the
# acceptance rows start from, so run make e2e-up again before them.
set -eu
# shellcheck source=../e2e/lib.sh
. "$(dirname "$0")/../e2e/lib.sh"
read_lab
act="$E2E_DIR/act.sh"

say 'task 1: storefront v3 waits for an approval'
plan=$(plan_of ptahschema storefront)
[ -n "$plan" ] || fail 'storefront waits for no plan; run make e2e-up again'

say 'task 2: journal applies a migration that reads a table that is not there'
"$act" fail journal
"$act" wait-run journal Failed

say 'task 3: the approver approves deliveries release 2, which stops part-way'
plan=$("$act" wait-plan ptahmigration deliveries)
migration=$(admin -n "$NAMESPACE" get ptahmigration deliveries -o json)
fingerprint=$(admin -n "$NAMESPACE" get ptahmigrationplan "$plan" -o jsonpath='{.spec.fingerprint}')
jq -n --arg namespace "$NAMESPACE" --arg plan "$plan" --arg fingerprint "$fingerprint" \
	--arg migration "$(printf '%s' "$migration" | jq -r .metadata.uid)" \
	--arg planUID "$(printf '%s' "$migration" | jq -r .status.plan.uid)" '{
    apiVersion: "operator.ptah.run/v1alpha1", kind: "PtahMigrationApproval",
    metadata: {generateName: "usability-deliveries-", namespace: $namespace},
    spec: {
      migrationRef: {name: "deliveries", uid: $migration},
      planRef: {name: $plan, uid: $planUID},
      planFingerprint: $fingerprint
    }
  }' | persona approver create -f - >/dev/null
"$act" wait-run deliveries Partial

say 'task 4: reports runs, and its Apply Job is deleted while it runs'
"$act" run reports
"$act" interrupt reports

say "ready: start a console for each task as docs/usability/early-adopters.md says"
