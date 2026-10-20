#!/bin/sh
# Brings the acceptance environment up: the operator's demonstration lab, at
# the operator commit go.mod requires, then this repository's personas and
# fixtures. Run it again on a lab that is up to reset the fixtures.
#
# The lab runs kind on a Docker context; DOCKER_CONTEXT names it and the lab
# defaults to remote-dev-container. It never falls back to a local daemon.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

if [ ! -d "$OPERATOR_DIR/.git" ]; then
	version=$(cd "$ROOT_DIR" && go list -m -f '{{.Version}}' github.com/stokaro/ptah-operator)
	revision=${version##*-}
	printf '%s\n' "$revision" | grep -Eq '^[0-9a-f]{12}$' || fail "cannot read a commit from $version"
	say "checking out the operator at $revision"
	git clone --quiet --filter=blob:none https://github.com/stokaro/ptah-operator.git "$OPERATOR_DIR"
	git -C "$OPERATOR_DIR" checkout --quiet "$revision"
fi
(cd "$OPERATOR_DIR" && make demo-up)
"$E2E_DIR/personas.sh"
"$E2E_DIR/fixtures.sh"
