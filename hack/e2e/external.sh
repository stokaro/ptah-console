#!/bin/sh
# The second cluster of the two-cluster rows (stokaro/ptah-console#102): a
# kind cluster beside the lab, on the same Docker network, with no setup for
# the console. It runs the operator from the lab's chart and images, a
# PostgreSQL of its own, and inventory, whose first plan waits for an
# approval. Its artifacts come from the lab's registry. The hosted console in
# the lab reaches it only through the agent its administrator installs.
#
#   external.sh up                   the hosted console with the agent
#                                    listener, and the target cluster
#   external.sh enroll <file> [values]
#                                    run the enrollment helper on the target
#   external.sh replay <file>        run the helper with the same file on the
#                                    lab's own cluster, and remove what it left
#   external.sh install [helm args]  install the agent from the helper's values
#   external.sh set <helm args>      upgrade the agent with changed values
#   external.sh credential <value>   write a channel credential into the
#                                    agent's Secret
#   external.sh restart              replace the agent's Pod
#   external.sh offline|online       freeze or release the target's node
#   external.sh probe <present> [probe flags]
#                                    open the agent channel presenting the
#                                    agent's credential, the bootstrap secret
#                                    of an enrollment file (bootstrap:<file>)
#                                    or garbage, and print the console's answer
#   external.sh can-i <can-i args>   what the agent's ServiceAccount may do
#   external.sh approvers <schema> [plan]
#                                    who admission recorded on the target's
#                                    approvals of a schema, or of one plan
#   external.sh insync <schema>      wait for a schema on the target
#   external.sh plan <schema>        print the plan a schema on the target
#                                    waits on
#   external.sh untouched            print the operator, inventory and its
#                                    database as they stand on the target
#   external.sh save <connection>    keep a registry entry as Git would hold it
#   external.sh restore <connection> create it again from what save kept,
#                                    status included, as a restore would
#   external.sh publish inventory-v2 point the target's inventory at v2 and
#                                    print the plan that waits for approval
#   external.sh credentials <connection>
#                                    the channel credentials the console
#                                    records: how many, used, retiring
#   external.sh misuse               present the agent's credential where a
#                                    person's session or another service is
#                                    expected, and print each answer
#   external.sh logs                 the agent's recent log
#   external.sh direct <connection> <uid>
#                                    try the registry's writes as accounts,
#                                    with kubectl and no console, dry-run
#   external.sh drop <connection>    delete a registry entry as its namespace's
#                                    administrator
#   external.sh refused <action...>  run an action that has to fail, and print
#                                    what it said
#   external.sh unenroll <uid>       remove the agent, the way the console's
#                                    cleanup steps say
#   external.sh down                 remove the target cluster and the image
#
# The rows reach the target's API server and the lab's worker over the
# Docker host's own network, which only a lab with direct host access has.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab

TARGET_CLUSTER="$E2E_KIND_CLUSTER_NAME-target"
TARGET_NODE="$TARGET_CLUSTER-control-plane"
TARGET_DIR="$OUT_DIR/target"
TARGET_KUBECONFIG="$TARGET_DIR/kubeconfig"
TARGET_CONTEXT="kind-$TARGET_CLUSTER"
AGENT_NS=ptah-console-agent
AGENT_RELEASE=ptah-console-agent
AGENT_IMAGE=ptah-console-agent:e2e
AGENT_VALUES="$TARGET_DIR/agent-values.yaml"
CONSOLE_NS=ptah-console
CONSOLE_BIN=${CONSOLE_BIN:-$ROOT_DIR/bin/ptah-console}

lab_docker() { DOCKER_CONTEXT=$E2E_DOCKER_CONTEXT DOCKER_CONFIG=$E2E_DOCKER_CONFIG "$@"; }
target() { kubectl --kubeconfig "$TARGET_KUBECONFIG" "$@"; }
helm_target() { go -C "$ROOT_DIR" tool -modfile=tools/chart/go.mod helm --kubeconfig "$TARGET_KUBECONFIG" "$@"; }

# on_target runs one of lib.sh's functions against the target cluster: they
# act through admin, which reads E2E_KUBECONFIG when it runs.
on_target() {
	(
		# shellcheck disable=SC2030 # Local to this subshell on purpose.
		E2E_KUBECONFIG=$TARGET_KUBECONFIG
		"$@"
	)
}

# registry_secrets writes the lab registry's credentials into a Secret of the
# target, as JSON built from the credentials file, so no password is an
# argument of a process.
registry_secrets() {
	jq -n \
		--arg namespace "$NAMESPACE" \
		--arg operatorNamespace "$E2E_OPERATOR_NAMESPACE" \
		--arg pullSecret "$(jq -er '.imagePullSecrets[0].name' "$E2E_CANDIDATE_VALUES_FILE")" \
		--arg registry "$E2E_REGISTRY_HOST" \
		--slurpfile credentials "$E2E_REGISTRY_CREDENTIALS_FILE" '
    $credentials[0] as $c |
    def pull($namespace; $name): {
      apiVersion: "v1", kind: "Secret", type: "kubernetes.io/dockerconfigjson",
      metadata: {namespace: $namespace, name: $name},
      data: {".dockerconfigjson": ({auths: {($registry): {
        username: $c.username, password: $c.password,
        auth: (($c.username + ":" + $c.password) | @base64)
      }}} | tojson | @base64)}
    };
    {
      apiVersion: "v1", kind: "List",
      items: [
        pull($operatorNamespace; $pullSecret),
        pull($namespace; "demo-registry-pull"),
        {
          apiVersion: "v1", kind: "Secret",
          metadata: {namespace: $namespace, name: "demo-registry"},
          stringData: {username: $c.username, password: $c.password, registry: $registry, allowPlainHTTP: "true"}
        }
      ]
    }' | target apply -f - >/dev/null
}

up() {
	[ -z "${E2E_TUNNEL_PID:-}" ] ||
		fail 'the two-cluster rows reach the clusters over the Docker host network; bring the lab up with direct host access'
	# Playwright runs the rows' beforeAll again in the worker it starts after a
	# failed row. The environment those rows left is what the next ones read,
	# so a second up changes nothing.
	if [ -f "$TARGET_DIR/up" ]; then
		say "the target cluster $TARGET_CLUSTER is already up"
		return 0
	fi
	mkdir -p "$TARGET_DIR"
	"$E2E_DIR/hosted.sh" agents

	if ! lab_docker kind get clusters 2>/dev/null | grep -qx "$TARGET_CLUSTER"; then
		node_image=$(lab_docker docker container inspect --format '{{.Config.Image}}' "$E2E_KIND_CLUSTER_NAME-control-plane")
		# One node, configured as the lab's are; no port is fixed, so kind
		# picks a free one for the API server.
		cat >"$TARGET_DIR/kind.yaml" <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  ipFamily: ipv4
  apiServerAddress: "127.0.0.1"
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: KubeletConfiguration
        apiVersion: kubelet.config.k8s.io/v1beta1
        featureGates:
          KubeletInUserNamespace: true
EOF
		say "creating the target cluster $TARGET_CLUSTER from $node_image"
		lab_docker kind create cluster --name "$TARGET_CLUSTER" --image "$node_image" \
			--config "$TARGET_DIR/kind.yaml" --kubeconfig "$TARGET_KUBECONFIG" --wait 5m >/dev/null
	fi
	fact TARGET_CONTEXT "$TARGET_CONTEXT"
	fact TARGET_KUBECONFIG "$TARGET_KUBECONFIG"

	say "pulling from the lab's registry, as the lab's nodes do"
	registry_dns=${E2E_REGISTRY_HOST%:*}
	deadline=$(($(date +%s) + 60))
	until lab_docker docker exec "$TARGET_NODE" getent ahostsv4 "$registry_dns" 2>/dev/null |
		awk -v expected="$E2E_REGISTRY_IP" '$1 == expected {found = 1} END {exit !found}'; do
		[ "$(date +%s)" -lt "$deadline" ] || fail "the target's node does not resolve $registry_dns to the registry"
		sleep 1
	done
	hosts_dir="/etc/containerd/certs.d/$E2E_REGISTRY_HOST"
	lab_docker docker exec "$E2E_KIND_CLUSTER_NAME-control-plane" cat "$hosts_dir/hosts.toml" >"$TARGET_DIR/hosts.toml"
	lab_docker docker exec "$TARGET_NODE" mkdir -p "$hosts_dir"
	lab_docker docker cp "$TARGET_DIR/hosts.toml" "$TARGET_NODE:$hosts_dir/hosts.toml" >/dev/null
	for namespace in "$E2E_OPERATOR_NAMESPACE" "$NAMESPACE"; do
		target create namespace "$namespace" --dry-run=client -o yaml | target apply -f - >/dev/null
	done
	# The registry is a container on the kind network. A Pod asks cluster DNS
	# for its .svc.cluster.local name, so the target gets the same route the
	# lab has: a Service and an EndpointSlice written by hand.
	jq -n --arg namespace "$NAMESPACE" --arg name "$E2E_REGISTRY_SERVICE" --arg address "$E2E_REGISTRY_IP" '
    {
      apiVersion: "v1", kind: "List",
      items: [
        {
          apiVersion: "v1", kind: "Service",
          metadata: {namespace: $namespace, name: $name},
          spec: {ports: [{name: "tcp", port: 5000, protocol: "TCP", targetPort: 5000}]}
        },
        {
          apiVersion: "discovery.k8s.io/v1", kind: "EndpointSlice",
          metadata: {namespace: $namespace, name: ($name + "-docker"), labels: {"kubernetes.io/service-name": $name}},
          addressType: "IPv4",
          endpoints: [{addresses: [$address]}],
          ports: [{name: "tcp", port: 5000, protocol: "TCP"}]
        }
      ]
    }' | target apply -f - >/dev/null
	registry_secrets
	target -n "$NAMESPACE" patch serviceaccount default --type=merge \
		--patch '{"imagePullSecrets":[{"name":"demo-registry-pull"}]}' >/dev/null

	if ! helm_target -n "$E2E_OPERATOR_NAMESPACE" status "$E2E_HELM_RELEASE" >/dev/null 2>&1; then
		say 'installing the operator from the lab chart and its values'
		helm_target install "$E2E_HELM_RELEASE" "$E2E_CHART_PACKAGE" --namespace "$E2E_OPERATOR_NAMESPACE" \
			--values "$E2E_CANDIDATE_VALUES_FILE" --wait --timeout 10m >/dev/null
	fi

	say 'inventory on the target, waiting for an approval of its first plan'
	jq -n --arg namespace "$NAMESPACE" --rawfile policy "$OPERATOR_DIR/demo/policy/verification-policy.yaml" '
    {
      apiVersion: "v1", kind: "ConfigMap",
      metadata: {namespace: $namespace, name: "demo-verification-policy"},
      immutable: true,
      data: {"policy.yaml": $policy}
    }' | target create -f - >/dev/null 2>&1 || true
	[ -s "$TARGET_DIR/postgres-password" ] ||
		head -c 18 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' >"$TARGET_DIR/postgres-password"
	password=$(cat "$TARGET_DIR/postgres-password")
	postgres_server target "$password"
	target -n "$NAMESPACE" exec deploy/e2e-postgres -- psql -U postgres -v ON_ERROR_STOP=1 -qAt \
		-c "DROP DATABASE IF EXISTS inventory WITH (FORCE)" -c "CREATE DATABASE inventory" >/dev/null
	target -n "$NAMESPACE" create secret generic e2e-database-inventory \
		--from-literal=url="postgres://postgres:$password@e2e-postgres.$NAMESPACE.svc.cluster.local:5432/inventory?sslmode=disable" \
		--dry-run=client -o yaml | target apply -f - >/dev/null
	APPLY=OnApproval INTERVAL=10m render inventory "$(digest inventory-v1)" | target apply -f - >/dev/null
	fact TARGET_INVENTORY_PLAN "$(on_target wait_plan ptahschema inventory)"
	fact APPROVER_CLUSTER_ROLE "$CONTROLLER-approver"

	say "building $AGENT_IMAGE for the target's node"
	arch=$(target get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
	lab_docker make -C "$ROOT_DIR" image-agent ARCH="$arch" AGENT_IMAGE="$AGENT_IMAGE" >/dev/null
	lab_docker kind load docker-image "$AGENT_IMAGE" --name "$TARGET_CLUSTER" >/dev/null
	touch "$TARGET_DIR/up"
	say "the target cluster $TARGET_CLUSTER is up; nothing on it knows the console yet"
}

# helper runs ptah-console cluster enroll with a kubeconfig and its context.
helper() {
	helper_kubeconfig=$1
	helper_context=$2
	shift 2
	"$CONSOLE_BIN" cluster enroll --kubeconfig "$helper_kubeconfig" --context "$helper_context" --yes "$@"
}

install() {
	[ -s "$AGENT_VALUES" ] || fail "no agent values at $AGENT_VALUES; enroll first"
	helm_target upgrade --install "$AGENT_RELEASE" "$ROOT_DIR/charts/ptah-console-agent" --namespace "$AGENT_NS" \
		--values "$AGENT_VALUES" \
		--set image.repository=ptah-console-agent --set image.tag=e2e --set image.pullPolicy=Never \
		"$@" --wait --timeout 3m >/dev/null
}

# presented writes what a probe presents as its bearer to a file of its own,
# and prints the file's name.
presented() {
	presented_file="$TARGET_DIR/presented"
	umask 077
	case "$1" in
	credential)
		target -n "$AGENT_NS" get secret "$AGENT_RELEASE-credential" -o jsonpath='{.data.credential}' |
			base64 -d >"$presented_file"
		;;
	bootstrap:*)
		sed -n 's/^bootstrap: *//p' "${1#bootstrap:}" | tr -d "\"'" >"$presented_file"
		;;
	garbage)
		head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' >"$presented_file"
		;;
	*) fail "present credential, bootstrap:<file> or garbage, not $1" ;;
	esac
	[ -s "$presented_file" ] || fail "nothing to present for $1"
	printf '%s\n' "$presented_file"
}

# attempt prints what the API server said to one request, without storing
# anything the request would have written.
attempt() {
	attempt_name=$1
	shift
	if attempt_said=$("$@" 2>&1); then
		printf '%s: admitted\n' "$attempt_name"
	else
		printf '%s: refused: %s\n' "$attempt_name" "$(printf '%s' "$attempt_said" | tr '\n' ' ')"
	fi
}

# as_account runs kubectl on the lab's cluster as a Console account's
# username, which the lab's administrator may impersonate.
as_account() {
	as_who=$1
	shift
	admin -n "$CONSOLE_NS" --as "ptah-console:e2e:$as_who" "$@"
}

# direct tries what the registry's schema and RBAC have to decide on their
# own, through kubectl and not the console: an administrator editing a
# connection's trust, naming it, writing a fence or a status; an ordinary
# account adding a connection; and who may read the connection.
direct() {
	direct_connection=$1
	direct_uid=$2
	printf '%s\n' "{\"apiVersion\": \"console.ptah.run/v1alpha1\", \"kind\": \"ConsoleClusterPreferences\",
 \"metadata\": {\"name\": \"$direct_connection\"},
 \"spec\": {\"clusterRef\": {\"name\": \"$direct_connection\", \"connectionUID\": \"$direct_uid\"}, \"displayName\": \"Second cluster\"}}" \
		>"$TARGET_DIR/direct-preferences.json"
	printf '%s\n' "{\"apiVersion\": \"console.ptah.run/v1alpha1\", \"kind\": \"ConsoleClusterRevocation\",
 \"metadata\": {\"name\": \"lifted-$direct_connection\"},
 \"spec\": {\"connectionUID\": \"$direct_uid\", \"action\": \"Quarantine\", \"generation\": 1,
          \"clusterName\": \"$direct_connection\", \"revokedBy\": \"ptah-console:e2e:admin\"}}" \
		>"$TARGET_DIR/direct-revocation.json"
	printf '%s\n' "{\"apiVersion\": \"console.ptah.run/v1alpha1\", \"kind\": \"ConsoleCluster\",
 \"metadata\": {\"name\": \"added-by-author\"},
 \"spec\": {\"connectionUID\": \"00000000-0000-4000-8000-000000000001\", \"profile\": \"CoLocated\",
          \"installationID\": \"e2e\", \"namespaces\": [\"$NAMESPACE\"], \"configurationOwner\": \"Console\"}}" \
		>"$TARGET_DIR/direct-connection.json"
	attempt admin-edits-trust as_account admin patch consolecluster "$direct_connection" \
		--type=merge --patch '{"spec":{"namespaces":["kube-system"]}}' --dry-run=server
	attempt admin-names-it as_account admin create --dry-run=server -f "$TARGET_DIR/direct-preferences.json"
	attempt admin-writes-a-revocation as_account admin create --dry-run=server -f "$TARGET_DIR/direct-revocation.json"
	attempt admin-writes-status as_account admin patch consolecluster "$direct_connection" \
		--subresource=status --type=merge --patch '{"status":{"channel":{"generation":9}}}' --dry-run=server
	attempt author-adds-a-connection as_account author create --dry-run=server -f "$TARGET_DIR/direct-connection.json"
	attempt author-reads-it as_account author get consolecluster "$direct_connection" -o name
	attempt newcomer-reads-it as_account newcomer get consolecluster "$direct_connection" -o name
}

[ "$#" -ge 1 ] || fail 'usage: external.sh <action> [arguments]; see the header'
action=$1
shift
case "$action" in
up) up ;;
enroll)
	[ -n "${1:-}" ] || fail 'enroll <enrollment file> [values path]'
	helper "$TARGET_KUBECONFIG" "$TARGET_CONTEXT" --enrollment-file "$1" --values "${2:-$AGENT_VALUES}"
	;;
replay)
	[ -n "${1:-}" ] || fail 'replay <enrollment file>'
	# shellcheck disable=SC2031 # The lab's own kubeconfig; on_target changed it in a subshell only.
	lab_context=$(kubectl --kubeconfig "$E2E_KUBECONFIG" config current-context)
	existed=$(admin get namespace "$AGENT_NS" -o name 2>/dev/null || true)
	status=0
	# shellcheck disable=SC2031 # As above.
	helper "$E2E_KUBECONFIG" "$lab_context" --enrollment-file "$1" --values "$TARGET_DIR/replay-values.yaml" 2>&1 || status=$?
	# The helper writes a credential before it asks the console; whatever the
	# console said, the lab's cluster keeps nothing of the attempt.
	[ -n "$existed" ] || admin delete namespace "$AGENT_NS" --ignore-not-found --wait=false >/dev/null
	exit "$status"
	;;
install) install "$@" ;;
set)
	helm_target upgrade "$AGENT_RELEASE" "$ROOT_DIR/charts/ptah-console-agent" --namespace "$AGENT_NS" \
		--reuse-values "$@" --wait --timeout 3m >/dev/null
	;;
credential)
	[ -n "${1:-}" ] || fail 'credential <value>'
	target -n "$AGENT_NS" create secret generic "$AGENT_RELEASE-credential" --from-literal=credential="$1" \
		--dry-run=client -o yaml | target apply -f - >/dev/null
	;;
restart)
	target -n "$AGENT_NS" rollout restart "deployment/$AGENT_RELEASE" >/dev/null
	target -n "$AGENT_NS" rollout status "deployment/$AGENT_RELEASE" --timeout=180s >/dev/null
	;;
offline) lab_docker docker pause "$TARGET_NODE" >/dev/null ;;
online) lab_docker docker unpause "$TARGET_NODE" >/dev/null ;;
probe)
	[ -n "${1:-}" ] || fail 'probe <credential|bootstrap:<file>|garbage> [probe flags]'
	file=$(presented "$1")
	shift
	go -C "$ROOT_DIR" run ./hack/e2e/probe -address "$(fact_of AGENTS_ADDRESS)" \
		-server-name "$(fact_of AGENTS_SERVER_NAME)" -ca "$(fact_of AGENTS_CA)" -credential-file "$file" "$@"
	rm -f "$file"
	;;
can-i)
	target auth can-i "$@" --as="system:serviceaccount:$AGENT_NS:$AGENT_RELEASE" 2>/dev/null || true
	;;
approvers)
	target -n "$NAMESPACE" get ptahschemaapprovals -o json |
		jq -r --arg schema "${1:?name a schema}" --arg plan "${2:-}" '
      [.items[] | select(.spec.schemaRef.name == $schema and ($plan == "" or .spec.planRef.name == $plan))
       | .spec.approver.username] | join(",")'
	;;
publish)
	[ "${1:-}" = inventory-v2 ] || fail 'only inventory-v2 is published on the target'
	if [ ! -s "$OUT_DIR/digests/inventory-v2" ]; then
		read_publisher
		push_schema inventory-v2 schemas/inventory "$E2E_DIR/schemas/inventory-v2.sql"
	fi
	before=$(on_target plan_of ptahschema inventory)
	APPLY=OnApproval INTERVAL=10m render inventory "$(digest inventory-v2)" | target apply -f - >/dev/null
	on_target wait_plan ptahschema inventory "$before"
	;;
credentials)
	admin -n "$CONSOLE_NS" get consolecluster "${1:?name a connection}" -o json | jq -r '
    .status.channel as $c |
    "generation \($c.generation) credentials \($c.credentials | length) used \([$c.credentials[] | select(.firstUsedAt)] | length) retiring \([$c.credentials[] | select(.retireAt)] | length)"'
	;;
misuse)
	file=$(presented credential)
	address=$(fact_of AGENTS_ADDRESS)
	authority=${address#https://}
	name=$(fact_of AGENTS_SERVER_NAME)
	# As a bearer on the browser's API, where only a session cookie counts. The
	# request is otherwise a valid Connect call, so the refusal is about the
	# credential and nothing else.
	printf 'console api: %s\n' "$(curl --silent --insecure --output /dev/null --write-out '%{http_code}' \
		--header "Authorization: Bearer $(cat "$file")" --header 'Content-Type: application/json' \
		--header 'Connect-Protocol-Version: 1' \
		--data '{}' "$(fact_of HOSTED_URL)/api/ptah.console.v1.ScopeService/ListNamespaces")"
	# On the agents' listener, which serves the channel and the enrollment
	# and nothing a person uses.
	printf 'listener decision: %s\n' "$(curl --silent --http2 --cacert "$(fact_of AGENTS_CA)" \
		--connect-to "$name:${authority##*:}:$authority" --output /dev/null --write-out '%{http_code}' \
		--header "Authorization: Bearer $(cat "$file")" --header 'Content-Type: application/json' \
		--data '{}' "https://$name:${authority##*:}/ptah.console.v1.DecisionService/ApproveSchemaPlan")"
	rm -f "$file"
	;;
logs) target -n "$AGENT_NS" logs "deployment/$AGENT_RELEASE" --tail=40 ;;
direct)
	direct "${1:?name a connection}" "${2:?give its connection UID}"
	;;
drop) admin -n "$CONSOLE_NS" delete consolecluster "${1:?name a connection}" --wait=true >/dev/null ;;
insync) on_target wait_condition ptahschema "${1:?name a schema}" InSync 900s ;;
plan) on_target plan_of ptahschema "${1:?name a schema}" ;;
untouched)
	target -n "$E2E_OPERATOR_NAMESPACE" get deployment "$CONTROLLER" -o json |
		jq -r '"operator \(.status.readyReplicas // 0)/\(.spec.replicas)"'
	target -n "$NAMESPACE" get ptahschema inventory -o json |
		jq -r '"inventory \(.status.phase // "-") \([.status.conditions[]? | select(.type == "InSync") | .status] | join(""))"'
	printf 'approvals %s\n' "$(target -n "$NAMESPACE" get ptahschemaapprovals -o name | wc -l | tr -d ' ')"
	printf 'tables %s\n' "$(target -n "$NAMESPACE" exec deploy/e2e-postgres -- psql -U postgres -d inventory -qAt \
		-c "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'")"
	;;
save)
	admin -n "$CONSOLE_NS" get consolecluster "${1:?name a connection}" -o json >"$TARGET_DIR/entry-$1.json"
	;;
restore)
	[ -s "$TARGET_DIR/entry-${1:?name a connection}.json" ] || fail "nothing saved of $1"
	jq 'del(.metadata.uid, .metadata.resourceVersion, .metadata.creationTimestamp, .metadata.generation,
	    .metadata.managedFields, .metadata.ownerReferences, .status)' "$TARGET_DIR/entry-$1.json" |
		admin create -f - >/dev/null
	jq --argjson current "$(admin -n "$CONSOLE_NS" get consolecluster "$1" -o json)" \
		'.metadata = $current.metadata' "$TARGET_DIR/entry-$1.json" |
		admin replace --subresource=status -f - >/dev/null
	;;
refused)
	status=0
	said=$("$0" "$@" 2>&1) || status=$?
	printf '%s\n' "$said"
	[ "$status" -ne 0 ] || fail "external.sh $* was expected to fail and did not"
	;;
unenroll)
	[ -n "${1:-}" ] || fail 'unenroll <connection UID>'
	target delete clusterrolebinding "$AGENT_RELEASE-impersonate" --ignore-not-found >/dev/null
	helm_target uninstall "$AGENT_RELEASE" --namespace "$AGENT_NS" --wait --timeout 3m >/dev/null
	"$CONSOLE_BIN" cluster unenroll --kubeconfig "$TARGET_KUBECONFIG" --context "$TARGET_CONTEXT" --connection-uid "$1"
	;;
down)
	lab_docker kind delete cluster --name "$TARGET_CLUSTER" >/dev/null 2>&1 || true
	lab_docker docker image rm --force "$AGENT_IMAGE" >/dev/null 2>&1 || true
	rm -rf "$TARGET_DIR"
	;;
*) fail "no action $action" ;;
esac
