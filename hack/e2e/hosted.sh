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
	admin -n "$CONSOLE_NS" port-forward "service/console-ptah-console" "$PORT:443" \
		>"$OUT_DIR/hosted-port-forward.log" 2>&1 &
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

# passwords writes the passwords Secret from the passwords on record.
passwords() {
	passwords_file="$OUT_DIR/hosted-passwords"
	: >"$passwords_file"
	for passwords_account in author approver; do
		passwords_hash=$(printf '%s\n' "$(cat "$OUT_DIR/hosted-$passwords_account.password")" |
			"$CONSOLE_BIN" accounts hash --cost 10)
		printf '%s:%s\n' "$passwords_account" "$passwords_hash" >>"$passwords_file"
	done
	admin -n "$CONSOLE_NS" create secret generic ptah-console-passwords \
		--from-file=passwords="$passwords_file" --dry-run=client -o yaml | admin apply -f - >/dev/null
	rm -f "$passwords_file"
}

# new_password records a fresh password for an account.
new_password() {
	umask 077
	openssl rand -base64 18 | tr -d '\n' >"$OUT_DIR/hosted-$1.password"
	fact "HOSTED_$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')_PASSWORD" "$(cat "$OUT_DIR/hosted-$1.password")"
}

up() {
	say "building $IMAGE for the lab's nodes"
	arch=$(admin get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
	lab_docker make -C "$ROOT_DIR" image ARCH="$arch" IMAGE="$IMAGE" >/dev/null
	lab_docker kind load docker-image "$IMAGE" --name "$E2E_KIND_CLUSTER_NAME" >/dev/null

	say 'issuing the console a certificate for localhost'
	admin create namespace "$CONSOLE_NS" --dry-run=client -o yaml | admin apply -f - >/dev/null
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
		-subj /CN=localhost -addext subjectAltName=DNS:localhost \
		-keyout "$OUT_DIR/hosted-tls.key" -out "$OUT_DIR/hosted-tls.crt" 2>/dev/null
	admin -n "$CONSOLE_NS" create secret tls ptah-console-tls \
		--cert="$OUT_DIR/hosted-tls.crt" --key="$OUT_DIR/hosted-tls.key" --dry-run=client -o yaml |
		admin apply -f - >/dev/null
	rm -f "$OUT_DIR/hosted-tls.key"
	new_password author
	new_password approver
	passwords

	say "installing the chart for $NAMESPACE"
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
		--set "accounts[1].roles={viewer,approver}" >/dev/null
	forward
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

[ "$#" -ge 1 ] || fail 'usage: hosted.sh up|restart|password <account>|uninstall'
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
uninstall)
	stop_forward
	helm_ uninstall "$RELEASE" --namespace "$CONSOLE_NS" --wait --timeout 3m >/dev/null
	;;
*) fail "unknown action $action" ;;
esac
