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

if [ -d "$OPERATOR_DIR/.git" ]; then
	"$E2E_DIR/operator.sh" --check "$OPERATOR_DIR"
else
	"$E2E_DIR/operator.sh" "$OPERATOR_DIR" >/dev/null
fi
(cd "$OPERATOR_DIR" && make demo-up)
"$E2E_DIR/personas.sh"
"$E2E_DIR/fixtures.sh"
