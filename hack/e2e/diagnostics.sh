#!/bin/sh
# Keeps the cluster's own account of an acceptance run in DIR, for a failure
# the rows and hack/e2e/verify.sh can only name: the Ptah resources as stored,
# the Events, the log and end state of every operation Pod still kept, the
# operator's log, the cluster DNS's log, the acceptance database server's
# connection log, and the probe that asked for that server once a second.
#
#   hack/e2e/diagnostics.sh DIR
#
# It reads no Secret. An operation Pod's runner keeps credentials out of its
# own output, and the database server logs names, never passwords. A part
# that cannot be read is named in DIR/missing.txt and the rest is kept.
set -u
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
read_lab
dir=${1:?usage: hack/e2e/diagnostics.sh DIR}
mkdir -p "$dir/operations"

keep() {
	keep_file=$1
	shift
	"$@" >"$dir/$keep_file" 2>&1 || printf '%s: %s\n' "$keep_file" "$*" >>"$dir/missing.txt"
}

keep resources.yaml admin -n "$NAMESPACE" get ptahschemas,ptahmigrations,ptahschemaapprovals,ptahmigrationapprovals -o yaml
keep pods.txt admin get pods -A -o wide
keep nodes.txt admin get nodes -o wide
keep events.txt admin -n "$NAMESPACE" get events --sort-by=.lastTimestamp \
	-o custom-columns=LAST:.lastTimestamp,TYPE:.type,REASON:.reason,OBJECT:.involvedObject.name,MESSAGE:.message
keep events-kube-system.txt admin -n kube-system get events --sort-by=.lastTimestamp \
	-o custom-columns=LAST:.lastTimestamp,TYPE:.type,REASON:.reason,OBJECT:.involvedObject.name,MESSAGE:.message
keep probe.log admin -n "$NAMESPACE" logs deployment/e2e-probe
keep postgres.log admin -n "$NAMESPACE" logs deployment/e2e-postgres --timestamps
keep coredns.log admin -n kube-system logs -l k8s-app=kube-dns --all-containers --prefix --timestamps --tail=-1
keep operator.log admin -n "$E2E_OPERATOR_NAMESPACE" logs -l app.kubernetes.io/component=controller \
	--all-containers --prefix --timestamps --tail=-1

admin -n "$NAMESPACE" get pods -l app.kubernetes.io/managed-by=ptah-operator \
	-o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null |
	while read -r pod; do
		[ -n "$pod" ] || continue
		keep "operations/$pod.log" admin -n "$NAMESPACE" logs "$pod" --all-containers --prefix --timestamps
		keep "operations/$pod.status.json" admin -n "$NAMESPACE" get pod "$pod" -o jsonpath='{.status}'
	done
printf 'diagnostics: kept the cluster account in %s\n' "$dir"
