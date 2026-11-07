#!/bin/sh
# Keeps the clusters' own account of an acceptance run in DIR, for a failure
# the rows and hack/e2e/verify.sh can only name: the Ptah resources as stored,
# the Events, the log and end state of every operation Pod still kept, the
# operator's log, the cluster DNS's log and the acceptance database server's
# connection log. The lab's account lands in DIR, with the probe that asked for
# that server once a second, and the second cluster's in DIR/second-cluster.
#
#   hack/e2e/diagnostics.sh DIR
#
# It reads no Secret. An operation Pod's runner keeps credentials out of its
# own output, and the database server logs names, never passwords. A part
# that cannot be read is named in DIR/missing.txt and the rest is kept.
#
# It runs after a failure of any kind, so a second cluster that was never
# created, or that a failed row left frozen, costs one line in missing.txt.
# Every read of a cluster has a bound: a read that hangs would hold the
# workflow's upload of what was already kept until the job's own timeout
# discarded it.
set -u
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab
dir=${1:?usage: hack/e2e/diagnostics.sh DIR}
mkdir -p "$dir"

keep() {
	keep_file=$1
	shift
	"$@" >"$dir/$keep_file" 2>&1 || printf '%s: %s\n' "$keep_file" "$*" >>"$dir/missing.txt"
}

# cluster reads the cluster E2E_KUBECONFIG names at the time it runs.
cluster() {
	admin --request-timeout=2m "$@"
}

# account keeps what both clusters have, under the path named first.
account() {
	account_at=$1
	mkdir -p "$dir/${account_at}operations"
	keep "${account_at}resources.yaml" cluster -n "$NAMESPACE" get ptahschemas,ptahmigrations,ptahschemaapprovals,ptahmigrationapprovals -o yaml
	keep "${account_at}pods.txt" cluster get pods -A -o wide
	keep "${account_at}nodes.txt" cluster get nodes -o wide
	keep "${account_at}events.txt" cluster -n "$NAMESPACE" get events --sort-by=.lastTimestamp \
		-o custom-columns=LAST:.lastTimestamp,TYPE:.type,REASON:.reason,OBJECT:.involvedObject.name,MESSAGE:.message
	keep "${account_at}events-kube-system.txt" cluster -n kube-system get events --sort-by=.lastTimestamp \
		-o custom-columns=LAST:.lastTimestamp,TYPE:.type,REASON:.reason,OBJECT:.involvedObject.name,MESSAGE:.message
	keep "${account_at}postgres.log" cluster -n "$NAMESPACE" logs deployment/e2e-postgres --timestamps
	keep "${account_at}coredns.log" cluster -n kube-system logs -l k8s-app=kube-dns --all-containers --prefix --timestamps --tail=-1
	keep "${account_at}operator.log" cluster -n "$E2E_OPERATOR_NAMESPACE" logs -l app.kubernetes.io/component=controller \
		--all-containers --prefix --timestamps --tail=-1

	cluster -n "$NAMESPACE" get pods -l app.kubernetes.io/managed-by=ptah-operator \
		-o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null |
		while read -r pod; do
			[ -n "$pod" ] || continue
			keep "${account_at}operations/$pod.log" cluster -n "$NAMESPACE" logs "$pod" --all-containers --prefix --timestamps
			keep "${account_at}operations/$pod.status.json" cluster -n "$NAMESPACE" get pod "$pod" -o jsonpath='{.status}'
		done
}

# second_cluster keeps the account of the cluster the two-cluster rows add,
# reached with the kubeconfig external.sh reaches it with. The first read has
# a short bound and decides: a node a failed row left frozen answers nothing,
# and waiting out every part's bound would say no more.
second_cluster() {
	read_target
	if [ ! -s "$TARGET_KUBECONFIG" ]; then
		printf 'second-cluster: no kubeconfig at %s; this run did not create it, or removed it\n' \
			"$TARGET_KUBECONFIG" >>"$dir/missing.txt"
		return 0
	fi
	# shellcheck disable=SC2030 # Local to the subshell this function runs in.
	E2E_KUBECONFIG=$TARGET_KUBECONFIG
	mkdir -p "$dir/second-cluster"
	if ! admin --request-timeout=20s get namespace "$NAMESPACE" -o name >"$dir/second-cluster/reachable.txt" 2>&1; then
		printf 'second-cluster: its API server did not answer; the reason is in second-cluster/reachable.txt, its node is %s\n' \
			"$(DOCKER_CONTEXT=$E2E_DOCKER_CONTEXT DOCKER_CONFIG=$E2E_DOCKER_CONFIG \
				docker container inspect --format '{{.State.Status}}' "$TARGET_NODE" 2>&1)" >>"$dir/missing.txt"
		return 0
	fi
	account second-cluster/
}

account ""
keep probe.log cluster -n "$NAMESPACE" logs deployment/e2e-probe
(second_cluster)
printf "diagnostics: kept the clusters' account in %s\n" "$dir"
