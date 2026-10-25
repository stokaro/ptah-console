#!/bin/sh
# OpenID Connect sign-in in the lab: a Dex every API server of the lab trusts
# as an issuer, and this repository's chart installed a second time with oidc
# on, served on this machine at OIDC_URL. A person signs in at Dex and acts in
# Kubernetes with their own ID token; the console impersonates nobody.
#
#   oidc.sh up     start Dex, make each API server trust it, install the
#                  console with oidc, and serve both on this machine
#   oidc.sh down   remove the console and Dex
#
# Dex's address is a cluster Service name, the one name the console's Pod,
# the API servers and the browser can all use: the Pod through cluster DNS,
# each API server through a line in its node's /etc/hosts, and the browser
# through a port-forward it reaches by a host resolver rule.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab

DEX_NS=ptah-oidc
DEX_HOST=dex.$DEX_NS.svc.cluster.local
DEX_PORT=5556
ISSUER="https://$DEX_HOST:$DEX_PORT/dex"
DEX_IMAGE=ghcr.io/dexidp/dex:v2.46.0@sha256:933fcd3f523338c847b88ef84a7145fb2fcb7b9917f08418612afeaaa2001777
CLIENT=ptah-console
CONSOLE_NS=ptah-console-oidc
RELEASE=console-oidc
PORT=18444
OIDC_URL="https://localhost:$PORT"
EMAIL=approver@e2e.test
USERNAME="oidc:$EMAIL"
IMAGE=ptah-console:e2e
CONSOLE_BIN=${CONSOLE_BIN:-$ROOT_DIR/bin/ptah-console}
AUTH_CONFIG=/etc/kubernetes/pki/ptah-console-oidc.yaml
MANIFEST=/etc/kubernetes/manifests/kube-apiserver.yaml
STATE="$OUT_DIR/oidc"

helm_() { go -C "$ROOT_DIR" tool -modfile=tools/chart/go.mod helm --kubeconfig "$E2E_KUBECONFIG" "$@"; }
lab_docker() { DOCKER_CONTEXT=$E2E_DOCKER_CONTEXT DOCKER_CONFIG=$E2E_DOCKER_CONFIG "$@"; }

# forward serves a Service on this machine, replacing an earlier forward of
# the same name.
forward() {
	forward_name=$1
	forward_ns=$2
	forward_target=$3
	forward_ports=$4
	if [ -s "$STATE/$forward_name.pid" ]; then
		kill "$(cat "$STATE/$forward_name.pid")" 2>/dev/null || true
	fi
	kubectl --kubeconfig "$E2E_KUBECONFIG" -n "$forward_ns" port-forward "$forward_target" "$forward_ports" \
		</dev/null >"$STATE/$forward_name.log" 2>&1 &
	echo $! >"$STATE/$forward_name.pid"
}

# certificates issues a CA, Dex's certificate for its Service name, and the
# console's for localhost.
certificates() {
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
		-subj "/CN=ptah-console e2e OIDC CA" -keyout "$STATE/ca.key" -out "$STATE/ca.crt" 2>/dev/null
	openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -subj "/CN=$DEX_HOST" \
		-keyout "$STATE/dex.key" -out "$STATE/dex.csr" 2>/dev/null
	printf 'subjectAltName=DNS:%s\n' "$DEX_HOST" >"$STATE/dex.ext"
	openssl x509 -req -in "$STATE/dex.csr" -CA "$STATE/ca.crt" -CAkey "$STATE/ca.key" -CAcreateserial \
		-days 1 -extfile "$STATE/dex.ext" -out "$STATE/dex.crt" 2>/dev/null
	openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 1 \
		-subj /CN=localhost -addext subjectAltName=DNS:localhost \
		-keyout "$STATE/console.key" -out "$STATE/console.crt" 2>/dev/null
}

# dex starts Dex with one user and the console as a public client, and
# waits until it serves its discovery document.
dex() {
	(
		umask 077
		openssl rand -base64 18 | tr -d '\n' >"$STATE/password"
	)
	hash=$(printf '%s\n' "$(cat "$STATE/password")" | "$CONSOLE_BIN" accounts hash --cost 10)
	admin create namespace "$DEX_NS" --dry-run=client -o yaml | admin apply -f - >/dev/null
	admin -n "$DEX_NS" create secret tls dex-tls --cert="$STATE/dex.crt" --key="$STATE/dex.key" \
		--dry-run=client -o yaml | admin apply -f - >/dev/null
	cat >"$STATE/dex.yaml" <<YAML
issuer: $ISSUER
storage:
  type: memory
web:
  https: 0.0.0.0:$DEX_PORT
  tlsCert: /etc/dex/tls/tls.crt
  tlsKey: /etc/dex/tls/tls.key
oauth2:
  skipApprovalScreen: true
  # The password grant lets this script check the API servers' trust before
  # a browser signs in; the console itself uses the code flow.
  passwordConnector: local
enablePasswordDB: true
staticPasswords:
  - email: $EMAIL
    hash: "$hash"
    username: approver
    userID: 6f0b2a51-6b6e-4b55-9d5c-2f1c9e4b7a10
staticClients:
  - id: $CLIENT
    name: Ptah Console
    public: true
    redirectURIs:
      - $OIDC_URL/auth/oidc/callback
YAML
	admin -n "$DEX_NS" create configmap dex --from-file=config.yaml="$STATE/dex.yaml" \
		--dry-run=client -o yaml | admin apply -f - >/dev/null
	admin apply -f - >/dev/null <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dex
  namespace: $DEX_NS
spec:
  replicas: 1
  selector:
    matchLabels: {app: dex}
  template:
    metadata:
      labels: {app: dex}
    spec:
      containers:
        - name: dex
          image: $DEX_IMAGE
          args: [dex, serve, /etc/dex/config/config.yaml]
          ports: [{containerPort: $DEX_PORT}]
          volumeMounts:
            - {name: config, mountPath: /etc/dex/config}
            - {name: tls, mountPath: /etc/dex/tls}
      volumes:
        - {name: config, configMap: {name: dex}}
        - {name: tls, secret: {secretName: dex-tls}}
---
apiVersion: v1
kind: Service
metadata:
  name: dex
  namespace: $DEX_NS
spec:
  selector: {app: dex}
  ports: [{port: $DEX_PORT, targetPort: $DEX_PORT}]
YAML
	admin -n "$DEX_NS" rollout restart deployment/dex >/dev/null
	admin -n "$DEX_NS" rollout status deployment/dex --timeout=300s >/dev/null ||
		fail 'Dex did not start'
	serve_dex
}

# serve_dex forwards Dex to this machine and waits until it serves its
# discovery document there. A port-forward runs through the API server, so it
# ends when the API server it went through restarts; trust starts it again.
serve_dex() {
	forward dex "$DEX_NS" service/dex "$DEX_PORT:$DEX_PORT"
	for _ in $(seq 1 60); do
		if curl --silent --fail --max-time 2 --cacert "$STATE/ca.crt" --resolve "$DEX_HOST:$DEX_PORT:127.0.0.1" \
			"$ISSUER/.well-known/openid-configuration" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	cat "$STATE/dex.log" >&2
	fail "Dex did not answer at $ISSUER"
}

# trust makes each API server of the lab trust Dex for the console's client,
# one node at a time, so the lab keeps answering while each restarts. The
# configuration lands in /etc/kubernetes/pki, which the API server already
# mounts, so only its flag changes.
trust() {
	address=$(admin -n "$DEX_NS" get service dex -o jsonpath='{.spec.clusterIP}')
	[ -n "$address" ] || fail 'Dex has no cluster address'
	ca=$(sed 's/^/          /' "$STATE/ca.crt")
	cat >"$STATE/authentication.yaml" <<YAML
apiVersion: apiserver.config.k8s.io/v1
kind: AuthenticationConfiguration
jwt:
  - issuer:
      url: $ISSUER
      audiences: [$CLIENT]
      certificateAuthority: |
$ca
    claimMappings:
      username:
        claim: email
        prefix: "oidc:"
YAML
	nodes=$(lab_docker docker ps --filter "label=io.x-k8s.kind.cluster=$E2E_KIND_CLUSTER_NAME" \
		--filter label=io.x-k8s.kind.role=control-plane --format '{{.Names}}' | sort)
	[ -n "$nodes" ] || fail "the lab has no control-plane node in $E2E_KIND_CLUSTER_NAME"
	printf '%s\n' "$nodes" | while read -r node; do
		lab_docker docker exec "$node" sh -c "grep -q ' $DEX_HOST\$' /etc/hosts || echo '$address $DEX_HOST' >> /etc/hosts"
		lab_docker docker exec -i "$node" sh -c "cat > $AUTH_CONFIG" <"$STATE/authentication.yaml"
		if ! lab_docker docker exec "$node" grep -q -- "--authentication-config=$AUTH_CONFIG" "$MANIFEST"; then
			say "restarting the API server on $node with the issuer it trusts"
			lab_docker docker exec -i "$node" sh -s "$MANIFEST" "$AUTH_CONFIG" <<'SH'
set -eu
# The flag goes right after the command's first word. kubeadm writes the
# manifest as YAML, and the lab may have written it again as JSON; either
# way the file is edited only where the command starts, and the edit is
# checked rather than assumed.
manifest=$1
flag="--authentication-config=$2"
if grep -q '^    - kube-apiserver$' "$manifest"; then
	sed -i "s|^    - kube-apiserver\$|    - kube-apiserver\n    - $flag|" "$manifest"
elif grep -q '^ *"kube-apiserver",$' "$manifest"; then
	sed -i "s|^\( *\)\"kube-apiserver\",\$|&\n\1\"$flag\",|" "$manifest"
fi
grep -q -- "$flag" "$manifest" || { echo "no place for $flag in $manifest" >&2; exit 1; }
SH
			# The kubelet replaces the static Pod; wait until the process that
			# runs carries the flag and answers. pgrep names the process, not a
			# command line, so it cannot match the shell that asks.
			deadline=$(($(date +%s) + 300))
			# shellcheck disable=SC2016 # The node's shell expands it.
			until lab_docker docker exec "$node" sh -c \
				'for pid in $(pgrep -x kube-apiserver); do tr "\000" " " </proc/$pid/cmdline; done | grep -q -- --authentication-config= &&
				curl -skf https://127.0.0.1:6443/readyz >/dev/null'; do
				[ "$(date +%s)" -lt "$deadline" ] || fail "the API server on $node did not come back with the issuer"
				sleep 3
			done
		fi
	done
	# The restarts ended the forward to Dex.
	serve_dex
	# The trust itself: a token Dex issues for the user reads back as the
	# user's username from the API server the console talks to.
	answer=$(curl --silent --show-error --cacert "$STATE/ca.crt" --resolve "$DEX_HOST:$DEX_PORT:127.0.0.1" \
		--data-urlencode grant_type=password --data-urlencode "username=$EMAIL" \
		--data-urlencode "password=$(cat "$STATE/password")" --data-urlencode "scope=openid email" \
		--data-urlencode "client_id=$CLIENT" "$ISSUER/token" 2>&1 || true)
	token=$(printf '%s' "$answer" | jq -r '.id_token // empty' 2>/dev/null || true)
	[ -n "$token" ] || fail "Dex issued no ID token for the user: $answer"
	# The lab's kubeconfig carries a client certificate, which the API server
	# would read first, so the token goes alone: the server and its CA from
	# the kubeconfig, and no other credential.
	server=$(kubectl --kubeconfig "$E2E_KUBECONFIG" config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')
	kubectl --kubeconfig "$E2E_KUBECONFIG" config view --raw --minify \
		-o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 -d >"$STATE/apiserver-ca.crt"
	: >"$STATE/empty.kubeconfig"
	deadline=$(($(date +%s) + 120))
	while :; do
		who=$(kubectl --kubeconfig "$STATE/empty.kubeconfig" --server "$server" \
			--certificate-authority "$STATE/apiserver-ca.crt" --token "$token" auth whoami \
			-o jsonpath='{.status.userInfo.username}' 2>/dev/null || true)
		[ "$who" = "$USERNAME" ] && break
		[ "$(date +%s)" -lt "$deadline" ] || fail "the API servers read Dex's token as '${who:-nobody}', not $USERNAME"
		sleep 3
	done
}

# console installs the chart with oidc for the lab namespace, the user bound
# to the viewer and approver roles, and serves it on this machine.
console() {
	# Built every time, as the hosted rows build it: an image an earlier run
	# left on the nodes is a console this tree did not build.
	image_arch=$(admin get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
	lab_docker make -C "$ROOT_DIR" image ARCH="$image_arch" IMAGE="$IMAGE" >/dev/null
	lab_docker kind load docker-image "$IMAGE" --name "$E2E_KIND_CLUSTER_NAME" >/dev/null
	admin create namespace "$CONSOLE_NS" --dry-run=client -o yaml | admin apply -f - >/dev/null
	admin -n "$CONSOLE_NS" create secret tls ptah-console-tls --cert="$STATE/console.crt" --key="$STATE/console.key" \
		--dry-run=client -o yaml | admin apply -f - >/dev/null
	helm_ upgrade --install "$RELEASE" "$ROOT_DIR/charts/ptah-console" --namespace "$CONSOLE_NS" --wait --timeout 5m \
		--set image.repository=ptah-console --set image.tag=e2e --set image.pullPolicy=Never \
		--set installationID=e2e-oidc --set publicURL="$OIDC_URL" \
		--set "namespaces={$NAMESPACE}" \
		--set approverClusterRole="$CONTROLLER-approver" \
		--set tls.secretName=ptah-console-tls \
		--set oidc.enabled=true --set oidc.issuerURL="$ISSUER" --set oidc.clientID="$CLIENT" \
		--set oidc.displayName="E2E Dex" --set-file oidc.issuerCA="$STATE/ca.crt" \
		--set "oidc.extraScopes={email}" \
		--set oidc.subjects[0].kind=User --set oidc.subjects[0].name="$USERNAME" \
		--set "oidc.subjects[0].roles={viewer,approver}" >/dev/null
	forward console "$CONSOLE_NS" "service/$RELEASE-ptah-console" "$PORT:443"
	for _ in $(seq 1 60); do
		if curl --silent --insecure --fail --max-time 2 "$OIDC_URL/healthz" >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	cat "$STATE/console.log" >&2
	fail "the OIDC console did not answer at $OIDC_URL"
}

case "${1:-}" in
up)
	mkdir -p "$STATE"
	say 'issuing certificates for Dex and the console'
	certificates
	say "starting Dex at $ISSUER"
	dex
	say "making the lab's API servers trust Dex"
	trust
	say "installing the console with oidc at $OIDC_URL"
	console
	fact OIDC_URL "$OIDC_URL"
	fact OIDC_EMAIL "$EMAIL"
	fact OIDC_USERNAME "$USERNAME"
	fact OIDC_PASSWORD "$(cat "$STATE/password")"
	say "ready: $USERNAME signs in at $OIDC_URL"
	;;
down)
	for name in console dex; do
		if [ -s "$STATE/$name.pid" ]; then
			kill "$(cat "$STATE/$name.pid")" 2>/dev/null || true
		fi
	done
	helm_ uninstall "$RELEASE" --namespace "$CONSOLE_NS" --wait >/dev/null 2>&1 || true
	admin delete namespace "$CONSOLE_NS" "$DEX_NS" --ignore-not-found --wait=false >/dev/null
	;;
*)
	fail 'usage: oidc.sh up|down'
	;;
esac
