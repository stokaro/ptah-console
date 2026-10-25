#!/bin/sh
# The hosted console in the lab: this repository's chart, installed with an
# author account and an approver account, the console's own image loaded into
# kind, and a port-forward that serves it on this machine at HOSTED_URL.
#
#   hosted.sh up                     build and load the image, install the
#                                    chart, and point inventory at a v2 that
#                                    waits for an approval
#   hosted.sh restart                replace the console's Pod
#   hosted.sh password <account>     give an account a new password, and roll
#                                    the console out so it takes effect
#   hosted.sh uninstall              remove the console
#   hosted.sh agents                 install it again with the registry, the
#                                    agent listener on a node port of the lab,
#                                    and an admin account, for the two-cluster
#                                    rows
#   hosted.sh newcomer               add an account after the agents were
#                                    enrolled, and roll the console out
#   hosted.sh logs                   print the console's latest log lines
#
# The image is built on the lab's Docker context, never another one.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab

RELEASE=console
CONSOLE_NS=ptah-console
DEPLOYMENT=deployment/console-ptah-console
IMAGE=ptah-console:e2e
PORT=18443
HOSTED_URL="https://localhost:$PORT"
INSTALLATION=e2e
CONSOLE_BIN=${CONSOLE_BIN:-$ROOT_DIR/bin/ptah-console}
FORWARD_PID="$OUT_DIR/hosted-port-forward.pid"

helm_() { go -C "$ROOT_DIR" tool -modfile=tools/chart/go.mod helm --kubeconfig "$E2E_KUBECONFIG" "$@"; }
lab_docker() { DOCKER_CONTEXT=$E2E_DOCKER_CONTEXT DOCKER_CONFIG=$E2E_DOCKER_CONFIG "$@"; }

# forward serves the console's Service on this machine, replacing an earlier
# forward, and waits until the console answers through it.
forward() {
	stop_forward
	# kubectl itself, not the admin function: dash runs a backgrounded function
	# in a subshell that keeps the caller's output open, so act.sh's caller
	# waited for it to end, and $! named the subshell rather than kubectl.
	kubectl --kubeconfig "$E2E_KUBECONFIG" -n "$CONSOLE_NS" port-forward \
		"service/console-ptah-console" "$PORT:443" </dev/null >"$OUT_DIR/hosted-port-forward.log" 2>&1 &
	echo $! >"$FORWARD_PID"
	for _ in $(seq 1 60); do
		if curl --silent --insecure --fail --max-time 2 "$HOSTED_URL/healthz" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	cat "$OUT_DIR/hosted-port-forward.log" >&2
	fail "the hosted console did not answer at $HOSTED_URL"
}

stop_forward() {
	if [ -s "$FORWARD_PID" ]; then
		kill "$(cat "$FORWARD_PID")" 2>/dev/null || true
		rm -f "$FORWARD_PID"
	fi
}

# rollout replaces the console's Pod and waits for the new one.
rollout() {
	admin -n "$CONSOLE_NS" rollout restart "$DEPLOYMENT" >/dev/null
	admin -n "$CONSOLE_NS" rollout status "$DEPLOYMENT" --timeout=180s >/dev/null
	forward
}

# passwords writes the passwords Secret from the passwords on record, one for
# each account new_password made.
passwords() {
	passwords_file="$OUT_DIR/hosted-passwords"
	: >"$passwords_file"
	find "$OUT_DIR" -maxdepth 1 -name 'hosted-*.password' | sort | while read -r passwords_path; do
		passwords_account=${passwords_path##*/hosted-}
		passwords_account=${passwords_account%.password}
		passwords_hash=$(printf '%s\n' "$(cat "$passwords_path")" | "$CONSOLE_BIN" accounts hash --cost 10)
		printf '%s:%s\n' "$passwords_account" "$passwords_hash" >>"$passwords_file"
	done
	admin -n "$CONSOLE_NS" create secret generic ptah-console-passwords \
		--from-file=passwords="$passwords_file" --dry-run=client -o yaml | admin apply -f - >/dev/null
	rm -f "$passwords_file"
}

# new_password records a fresh password for an account.
new_password() {
	# Only the password file is private; a umask left behind would make every
	# file the script writes after it private too, the image's binary included.
	(
		umask 077
		openssl rand -base64 18 | tr -d '\n' >"$OUT_DIR/hosted-$1.password"
	)
	fact "HOSTED_$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')_PASSWORD" "$(cat "$OUT_DIR/hosted-$1.password")"
}

# image builds the console's image for the lab's nodes and loads it into kind.
image() {
	say "building $IMAGE for the lab's nodes"
	image_arch=$(admin get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
	lab_docker make -C "$ROOT_DIR" image ARCH="$image_arch" IMAGE="$IMAGE" >/dev/null
	lab_docker kind load docker-image "$IMAGE" --name "$E2E_KIND_CLUSTER_NAME" >/dev/null
}

# secrets issues the console a certificate for localhost and writes the
# passwords of the accounts on record.
secrets() {
	admin create namespace "$CONSOLE_NS" --dry-run=client -o yaml | admin apply -f - >/dev/null
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
		-subj /CN=localhost -addext subjectAltName=DNS:localhost \
		-keyout "$OUT_DIR/hosted-tls.key" -out "$OUT_DIR/hosted-tls.crt" 2>/dev/null
	admin -n "$CONSOLE_NS" create secret tls ptah-console-tls \
		--cert="$OUT_DIR/hosted-tls.crt" --key="$OUT_DIR/hosted-tls.key" --dry-run=client -o yaml |
		admin apply -f - >/dev/null
	rm -f "$OUT_DIR/hosted-tls.key"
	passwords
}

# install installs the chart for the lab namespace with the author and the
# approver accounts, or upgrades it, with the values given after them.
install() {
	helm_ upgrade --install "$RELEASE" "$ROOT_DIR/charts/ptah-console" --namespace "$CONSOLE_NS" --wait --timeout 5m \
		--set image.repository=ptah-console --set image.tag=e2e --set image.pullPolicy=Never \
		--set installationID="$INSTALLATION" --set publicURL="$HOSTED_URL" \
		--set "namespaces={$NAMESPACE}" \
		--set approverClusterRole="$CONTROLLER-approver" \
		--set passwords.secretName=ptah-console-passwords --set tls.secretName=ptah-console-tls \
		--set accounts[0].id=author --set accounts[0].login=author@e2e.test \
		--set accounts[0].displayName="Avery Author" \
		--set "accounts[0].roles={viewer,desired-state-author,plan-reader}" \
		--set accounts[1].id=approver --set accounts[1].login=approver@e2e.test \
		--set accounts[1].displayName="Arden Approver" \
		--set "accounts[1].roles={viewer,approver}" "$@" >/dev/null
	forward
}

up() {
	image
	say 'issuing the console a certificate for localhost'
	new_password author
	new_password approver
	secrets

	say "installing the chart for $NAMESPACE"
	install
	fact HOSTED_URL "$HOSTED_URL"
	fact HOSTED_AUTHOR "ptah-console:$INSTALLATION:author"
	fact HOSTED_APPROVER "ptah-console:$INSTALLATION:approver"

	say 'inventory v2, written by the administrator, waits for an approval'
	read_publisher
	push_schema inventory-v2 schemas/inventory "$E2E_DIR/schemas/inventory-v2.sql"
	before=$(plan_of ptahschema inventory)
	APPLY=OnApproval INTERVAL=10m render inventory "$(digest inventory-v2)" | admin apply -f - >/dev/null
	fact HOSTED_INVENTORY_PLAN "$(wait_plan ptahschema inventory "$before")"
	say "the hosted console serves $HOSTED_URL"
}

# The agent listener: a node port on the lab's worker, which the second
# cluster's nodes reach over the kind network, and a certificate for the name
# agents are enrolled with, from a CA of the console's own.
AGENTS_NAME=agents.console.e2e.test
AGENTS_NODE_PORT=31443
AGENTS_CA="$OUT_DIR/agents-ca.crt"

# agents_ca issues the listener its certificate, once.
agents_ca() {
	[ -s "$AGENTS_CA" ] && return 0
	umask 077
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 2 \
		-subj '/CN=Ptah Console e2e agents CA' \
		-addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign \
		-keyout "$OUT_DIR/agents-ca.key" -out "$AGENTS_CA" 2>/dev/null
	openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -subj "/CN=$AGENTS_NAME" \
		-keyout "$OUT_DIR/agents-tls.key" -out "$OUT_DIR/agents-tls.csr" 2>/dev/null
	printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\n' "$AGENTS_NAME" >"$OUT_DIR/agents-tls.ext"
	openssl x509 -req -in "$OUT_DIR/agents-tls.csr" -CA "$AGENTS_CA" -CAkey "$OUT_DIR/agents-ca.key" \
		-CAcreateserial -days 1 -extfile "$OUT_DIR/agents-tls.ext" -out "$OUT_DIR/agents-tls.crt" 2>/dev/null
	admin -n "$CONSOLE_NS" create secret tls ptah-console-agents-tls \
		--cert="$OUT_DIR/agents-tls.crt" --key="$OUT_DIR/agents-tls.key" --dry-run=client -o yaml |
		admin apply -f - >/dev/null
	rm -f "$OUT_DIR/agents-ca.key" "$OUT_DIR/agents-tls.key" "$OUT_DIR/agents-tls.csr"
}

# install_agents installs the console with the registry and the listener, the
# author and the approver accounts, and an installation administrator, with
# the values given after them.
install_agents() {
	node_ip=$(DOCKER_CONFIG=$E2E_DOCKER_CONFIG docker --context "$E2E_DOCKER_CONTEXT" container inspect \
		--format '{{with index .NetworkSettings.Networks "kind"}}{{.IPAddress}}{{end}}' "$E2E_KIND_CLUSTER_NAME-worker")
	printf '%s\n' "$node_ip" | grep -Eq '^[0-9]+(\.[0-9]+){3}$' || fail "the lab's worker has no address on the kind network"
	address="https://$node_ip:$AGENTS_NODE_PORT"
	install --set registry.enabled=true --set agents.enabled=true \
		--set agents.address="$address" --set agents.serverName="$AGENTS_NAME" \
		--set-file agents.caBundle="$AGENTS_CA" --set agents.tls.secretName=ptah-console-agents-tls \
		--set agents.service.type=NodePort --set agents.service.nodePort="$AGENTS_NODE_PORT" \
		--set accounts[2].id=admin --set accounts[2].login=admin@e2e.test \
		--set accounts[2].displayName="Ada Admin" \
		--set "accounts[2].roles={viewer,connection-admin}" "$@"
	fact AGENTS_ADDRESS "$address"
	fact AGENTS_SERVER_NAME "$AGENTS_NAME"
	fact AGENTS_CA "$AGENTS_CA"
}

agents() {
	admin -n "$CONSOLE_NS" get secret ptah-console-tls >/dev/null 2>&1 || {
		image
		for agents_account in author approver; do
			[ -s "$OUT_DIR/hosted-$agents_account.password" ] || new_password "$agents_account"
		done
		fact HOSTED_URL "$HOSTED_URL"
		fact HOSTED_AUTHOR "ptah-console:$INSTALLATION:author"
		fact HOSTED_APPROVER "ptah-console:$INSTALLATION:approver"
	}
	new_password admin
	fact HOSTED_ADMIN "ptah-console:$INSTALLATION:admin"
	secrets
	agents_ca
	say 'installing the chart with the registry and the agent listener'
	install_agents
}

# newcomer adds an account the target's agent was never told of.
newcomer() {
	new_password newcomer
	fact HOSTED_NEWCOMER "ptah-console:$INSTALLATION:newcomer"
	passwords
	# The passwords are read when the Pod starts. The new account changes the
	# configuration, and the chart rolls the console out for that.
	install_agents --set accounts[3].id=newcomer --set accounts[3].login=newcomer@e2e.test \
		--set accounts[3].displayName="Noor Newcomer" --set "accounts[3].roles={viewer}"
}

[ "$#" -ge 1 ] || fail 'usage: hosted.sh up|restart|password <account>|uninstall|agents|newcomer|logs'
action=$1
shift
case "$action" in
up) up ;;
restart) rollout ;;
password)
	case "${1:-}" in
	author | approver) ;;
	*) fail 'password author or password approver' ;;
	esac
	cp "$OUT_DIR/hosted-$1.password" "$OUT_DIR/hosted-$1.password.old"
	new_password "$1"
	fact "HOSTED_$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')_OLD_PASSWORD" "$(cat "$OUT_DIR/hosted-$1.password.old")"
	passwords
	rollout
	;;
agents) agents ;;
newcomer) newcomer ;;
logs) admin -n "$CONSOLE_NS" logs "$DEPLOYMENT" --tail=80 ;;
uninstall)
	stop_forward
	helm_ uninstall "$RELEASE" --namespace "$CONSOLE_NS" --wait --timeout 3m >/dev/null
	;;
*) fail "unknown action $action" ;;
esac
