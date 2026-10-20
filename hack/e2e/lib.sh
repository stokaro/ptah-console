# Shared by the acceptance scripts: the lab's environment, the commands that
# act as its administrator, and the facts the scripts hand to the browser.
#
#   OPERATOR_DIR  the ptah-operator checkout whose demonstration lab is up
#                 (default .e2e/operator)
#   OUT_DIR       where kubeconfigs, digests and facts are written
#                 (default .e2e/out)

ROOT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
E2E_DIR="$ROOT_DIR/hack/e2e"
OPERATOR_DIR=${OPERATOR_DIR:-$ROOT_DIR/.e2e/operator}
OUT_DIR=${OUT_DIR:-$ROOT_DIR/.e2e/out}
LAB="$OPERATOR_DIR/demo/bin/lab"
LAB_ENVIRONMENT=${LAB_ENVIRONMENT:-$OPERATOR_DIR/demo/.lab/environment}

say() {
	printf 'e2e: %s\n' "$1"
}

fail() {
	printf 'e2e: %s\n' "$1" >&2
	exit 1
}

# read_lab loads what the operator's lab bootstrap wrote.
read_lab() {
	[ -f "$LAB_ENVIRONMENT" ] || fail "no lab environment at $LAB_ENVIRONMENT; run make e2e-up first"
	set -a
	# shellcheck disable=SC1090 # The lab's NAME=value file.
	. "$LAB_ENVIRONMENT"
	set +a
	NAMESPACE=${E2E_TEST_NAMESPACE:?the lab names no namespace}
	# shellcheck disable=SC2034 # Read by the scripts that source this file.
	CONTROLLER=${E2E_CONTROLLER_NAME:?the lab names no controller}
	mkdir -p "$OUT_DIR/digests"
}

# read_publisher makes `ptah` and the registry credentials available.
read_publisher() {
	eval "$("$LAB" credentials)"
	export PTAH_OCI_USERNAME PTAH_OCI_PASSWORD PTAH_OCI_REGISTRY
	PATH="$("$LAB" tools):$PATH"
	export PATH
}

admin() {
	kubectl --kubeconfig "$E2E_KUBECONFIG" "$@"
}

# persona runs kubectl as one persona, with the kubeconfig personas.sh wrote.
persona() {
	persona_who=$1
	shift
	kubectl --kubeconfig "$OUT_DIR/$persona_who.kubeconfig" "$@"
}

# fact records one NAME=value for the rows that follow, replacing an earlier
# value of the same name.
fact() {
	touch "$OUT_DIR/facts"
	grep -v "^$1=" "$OUT_DIR/facts" >"$OUT_DIR/facts.new" || true
	printf '%s=%s\n' "$1" "$2" >>"$OUT_DIR/facts.new"
	mv "$OUT_DIR/facts.new" "$OUT_DIR/facts"
}

# digest prints the digest an earlier push recorded under a name.
digest() {
	[ -s "$OUT_DIR/digests/$1" ] || fail "nothing was published as $1"
	cat "$OUT_DIR/digests/$1"
}

# record_digest keeps the digest a push printed, and refuses a push that
# printed none.
record_digest() {
	record_name=$1
	sed -n 's/^Digest: //p' >"$OUT_DIR/digests/$record_name"
	case "$(cat "$OUT_DIR/digests/$record_name")" in
	sha256:*) ;;
	*) fail "publishing $record_name returned no digest" ;;
	esac
}

# push_schema publishes one desired schema file as an artifact.
push_schema() {
	push_name=$1
	push_repository=$2
	push_file=$3
	ptah schema push "oci://$PTAH_OCI_REGISTRY/$push_repository:$push_name-$$" \
		--schema-file "$push_file" --dialect postgres --plain-http | record_digest "$push_name"
}

# push_migrations publishes a migration directory as an artifact.
push_migrations() {
	push_name=$1
	push_repository=$2
	push_dir=$3
	ptah migrations push "oci://$PTAH_OCI_REGISTRY/$push_repository:$push_name-$$" \
		--migrations-dir "$push_dir" --dir-format ptah --version "$push_name-$$" --plain-http |
		record_digest "$push_name"
}

# render prints one of this repository's manifest templates for the lab, the
# way the lab's own `manifest` command renders its templates.
render() {
	render_template="$E2E_DIR/manifests/$1.yaml"
	[ -f "$render_template" ] || fail "no manifest template at $render_template"
	sed \
		-e "s|\${NAMESPACE}|$NAMESPACE|g" \
		-e "s|\${REGISTRY}|$E2E_REGISTRY_HOST|g" \
		-e "s|\${DIGEST}|$2|g" \
		-e "s|\${APPLY}|${APPLY:?set APPLY}|g" \
		-e "s|\${INTERVAL}|${INTERVAL:-10m}|g" \
		"$render_template"
}

# wait_condition waits for a condition on one resource and prints its
# conditions when it never comes.
wait_condition() {
	wait_kind=$1
	wait_name=$2
	wait_condition=$3
	wait_timeout=${4:-600s}
	if ! admin -n "$NAMESPACE" wait --for="condition=$wait_condition" --timeout="$wait_timeout" \
		"$wait_kind/$wait_name" >/dev/null; then
		admin -n "$NAMESPACE" get "$wait_kind/$wait_name" -o json |
			jq -r '"phase \(.status.phase // "-")", (.status.conditions[]? | "\(.type)=\(.status) \(.reason): \(.message)")' >&2 || true
		fail "$wait_kind/$wait_name never reached $wait_condition"
	fi
}

# plan_of prints the name of the plan a resource waits on, or nothing.
plan_of() {
	admin -n "$NAMESPACE" get "$1/$2" -o jsonpath='{.status.plan.name}'
}

# wait_plan waits until a resource waits for approval of a published plan
# other than the one named, and prints that plan's name.
wait_plan() {
	wait_plan_kind=$1
	wait_plan_name=$2
	wait_plan_other=${3:-}
	wait_plan_deadline=$(($(date +%s) + 600))
	while [ "$(date +%s)" -lt "$wait_plan_deadline" ]; do
		wait_plan_current=$(plan_of "$wait_plan_kind" "$wait_plan_name")
		wait_plan_required=$(admin -n "$NAMESPACE" get "$wait_plan_kind/$wait_plan_name" \
			-o jsonpath='{.status.conditions[?(@.type=="ApprovalRequired")].status}')
		if [ -n "$wait_plan_current" ] && [ "$wait_plan_current" != "$wait_plan_other" ] &&
			[ "$wait_plan_required" = True ]; then
			printf '%s\n' "$wait_plan_current"
			return 0
		fi
		sleep 3
	done
	fail "$wait_plan_kind/$wait_plan_name published no new plan to approve"
}

# postgres runs psql as the superuser of the acceptance database server.
postgres() {
	admin -n "$NAMESPACE" exec deploy/e2e-postgres -- psql -U postgres -v ON_ERROR_STOP=1 -qAt "$@"
}

# mysql_demo runs statements in the lab's MySQL demonstration database.
mysql_demo() {
	admin -n "$NAMESPACE" exec deploy/demo-mysql-client -- mysql demo -N -e "$1"
}
