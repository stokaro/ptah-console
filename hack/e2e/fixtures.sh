#!/bin/sh
# Puts the resources the acceptance rows read into the lab, and waits until
# each is in the state its rows start from:
#
#   storefront  waits for an approval of a plan that drops a column and grants
#               SELECT to every role; the lead persona wrote that spec
#   inventory   InSync, applied without an approval
#   billing     migrations 1-13 applied, 14-17 waiting for an approval
#   deliveries  MySQL; release 1 applied, release 2 waiting for an approval
#
# The installation is switched to the four-eyes control first, so every
# resource written here has a recorded writer for admission to compare an
# approver with. personas.sh has to have run: the lead writes storefront.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab
read_publisher
[ -s "$OUT_DIR/lead.kubeconfig" ] || fail "no personas in $OUT_DIR; run personas.sh first"

say 'turning on the four-eyes control in the installed release'
CHART=${E2E_CHART_PACKAGE:?the lab names no chart package}
[ -f "$CHART" ] || fail "the chart package $CHART is gone"
if [ "$(helm --kubeconfig "$E2E_KUBECONFIG" -n "$E2E_OPERATOR_NAMESPACE" get values "$E2E_HELM_RELEASE" \
	-o json | jq -r '.approvals.requireDistinctApprover // false')" != true ]; then
	helm --kubeconfig "$E2E_KUBECONFIG" -n "$E2E_OPERATOR_NAMESPACE" upgrade "$E2E_HELM_RELEASE" "$CHART" \
		--reuse-values --set approvals.requireDistinctApprover=true --wait --timeout 10m >/dev/null
fi
admin get mutatingwebhookconfigurations -o json |
	jq -e '[.items[].webhooks[]?.name] | index("mschemawriter.operator.ptah.run")' >/dev/null ||
	fail 'the release installed no spec-writer webhook, so four-eyes would refuse every approval'

say 'returning the namespace to an empty state'
"$LAB" reset
admin -n "$NAMESPACE" delete ptahmigrationrunacknowledgment --all --ignore-not-found --timeout=60s >/dev/null

say 'a PostgreSQL server for the databases this repository adds'
[ -s "$OUT_DIR/postgres-password" ] ||
	head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' >"$OUT_DIR/postgres-password"
PASSWORD=$(cat "$OUT_DIR/postgres-password")
admin -n "$NAMESPACE" create secret generic e2e-postgres --from-literal=password="$PASSWORD" \
	--dry-run=client -o yaml | admin apply -f - >/dev/null
jq -n --arg namespace "$NAMESPACE" --arg image "$E2E_POSTGRES_IMAGE" '
  def labels: {"app.kubernetes.io/name": "e2e-postgres"};
  {
    apiVersion: "v1", kind: "List",
    items: [
      {
        apiVersion: "apps/v1", kind: "Deployment",
        metadata: {namespace: $namespace, name: "e2e-postgres"},
        spec: {
          replicas: 1,
          selector: {matchLabels: labels},
          template: {
            metadata: {labels: labels},
            spec: {
              automountServiceAccountToken: false,
              containers: [{
                name: "postgres", image: $image, imagePullPolicy: "IfNotPresent",
                env: [{name: "POSTGRES_PASSWORD", valueFrom: {secretKeyRef: {name: "e2e-postgres", key: "password"}}}],
                ports: [{name: "postgresql", containerPort: 5432}],
                readinessProbe: {exec: {command: ["pg_isready", "-h", "127.0.0.1", "-U", "postgres"]}, periodSeconds: 3}
              }]
            }
          }
        }
      },
      {
        apiVersion: "v1", kind: "Service",
        metadata: {namespace: $namespace, name: "e2e-postgres"},
        spec: {selector: labels, ports: [{name: "postgresql", port: 5432, targetPort: "postgresql"}]}
      }
    ]
  }' | admin apply -f - >/dev/null
admin -n "$NAMESPACE" rollout status deployment/e2e-postgres --timeout=180s >/dev/null
for database in inventory billing journal; do
	postgres -c "DROP DATABASE IF EXISTS $database WITH (FORCE)" -c "CREATE DATABASE $database"
	admin -n "$NAMESPACE" create secret generic "e2e-database-$database" \
		--from-literal=url="postgres://postgres:$PASSWORD@e2e-postgres.$NAMESPACE.svc.cluster.local:5432/$database?sslmode=disable" \
		--dry-run=client -o yaml | admin apply -f - >/dev/null
done

say 'publishing the artifacts'
push_schema storefront-v2 schemas/demo "$OPERATOR_DIR/demo/schemas/v2.sql"
for version in v3 v4 v5; do
	push_schema "storefront-$version" schemas/demo "$E2E_DIR/schemas/storefront-$version.sql"
done
push_schema inventory-v1 schemas/inventory "$E2E_DIR/schemas/inventory-v1.sql"
STAGE="$OUT_DIR/stage"
rm -rf "$STAGE"
mkdir -p "$STAGE/billing-release-1"
find "$E2E_DIR/migrations/billing" -name '00000000[01][0-9]_*' | while read -r file; do
	case "${file##*/}" in
	000000001[4-9]_*) ;;
	*) cp "$file" "$STAGE/billing-release-1/" ;;
	esac
done
[ "$(find "$STAGE/billing-release-1" -name '*.up.sql' | wc -l | tr -d ' ')" = 13 ] ||
	fail 'billing release 1 does not hold migrations 1 to 13'
push_migrations billing-release-1 migrations/billing "$STAGE/billing-release-1"
push_migrations billing-release-2 migrations/billing "$E2E_DIR/migrations/billing"
push_migrations journal-release-1 migrations/journal "$E2E_DIR/migrations/journal"
for release in 1 2; do
	push_migrations "deliveries-release-$release" migrations/deliveries "$OPERATOR_DIR/demo/mysql/release-$release"
done

# Each resource applies on its own interval; ten minutes keeps a plan that
# waits for approval published while a row reviews it.
export INTERVAL=10m

say 'inventory, applied without an approval'
APPLY=Always render inventory "$(digest inventory-v1)" | admin apply -f - >/dev/null
say 'storefront v2, applied without an approval'
APPLY=Always "$LAB" manifest storefront "$(digest storefront-v2)" | admin apply -f - >/dev/null
say 'billing migrations 1 to 13, applied without an approval'
APPLY=Always render billing "$(digest billing-release-1)" | admin apply -f - >/dev/null
say 'deliveries release 1 on MySQL, applied without an approval'
APPLY=Always "$LAB" manifest deliveries "$(digest deliveries-release-1)" | admin apply -f - >/dev/null

wait_condition ptahschema inventory InSync
wait_condition ptahschema storefront InSync
wait_condition ptahmigration billing Ready
wait_condition ptahmigration deliveries Ready
mysql_demo "INSERT INTO deliveries VALUES (1, 'D-100'), (2, 'D-200'), (3, 'D-300')"

say 'storefront v3, written by the lead: it drops a column and grants SELECT'
APPLY=OnApproval ALLOW_DESTRUCTIVE=true "$LAB" manifest storefront "$(digest storefront-v3)" |
	persona lead apply -f - >/dev/null
WRITER=$(admin -n "$NAMESPACE" get ptahschema storefront -o json |
	jq -r '.metadata.annotations["operator.ptah.run/last-spec-writer-username"] // ""')
[ "$WRITER" = e2e-lead ] || fail "admission recorded ${WRITER:-nobody} as storefront's writer, not e2e-lead"
say 'billing migrations 14 to 17, waiting for an approval'
APPLY=OnApproval render billing "$(digest billing-release-2)" | admin apply -f - >/dev/null
say 'deliveries release 2, waiting for an approval'
APPLY=OnApproval "$LAB" manifest deliveries "$(digest deliveries-release-2)" | admin apply -f - >/dev/null

STOREFRONT_PLAN=$(wait_plan ptahschema storefront)
admin -n "$NAMESPACE" wait --for=condition=Ready --timeout=120s "ptahschemaplan/$STOREFRONT_PLAN" >/dev/null ||
	fail "the plan $STOREFRONT_PLAN did not report its storage ready"
admin -n "$NAMESPACE" get "ptahschemaplan/$STOREFRONT_PLAN" -o json |
	jq -e '.spec.destructive == true and (.spec.privilegeChanges | index("Grant"))' >/dev/null ||
	fail "the plan $STOREFRONT_PLAN is not destructive with a Grant"
BILLING_PLAN=$(wait_plan ptahmigration billing)
admin -n "$NAMESPACE" get "ptahmigrationplan/$BILLING_PLAN" -o json |
	jq -e '[.spec.migrations[].version] == [14, 15, 16, 17]' >/dev/null ||
	fail "the plan $BILLING_PLAN does not hold migrations 14 to 17"
DELIVERIES_PLAN=$(wait_plan ptahmigration deliveries)

fact STOREFRONT_V3_PLAN "$STOREFRONT_PLAN"
fact BILLING_PLAN "$BILLING_PLAN"
fact DELIVERIES_PLAN "$DELIVERIES_PLAN"
say "ready: storefront waits for $STOREFRONT_PLAN, inventory is InSync"
