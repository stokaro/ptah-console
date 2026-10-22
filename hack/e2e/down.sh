#!/bin/sh
# Removes the acceptance environment: the operator's lab, with every container
# and volume it created, the console image the hosted rows built, the second
# cluster the two-cluster rows added, and the kubeconfigs this repository
# wrote.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

# The hosted rows built the console's image on the lab's Docker context; it is
# removed there, by name, before the lab that names the context goes, and so
# are the two-cluster rows' target cluster and agent image.
if [ -f "$LAB_ENVIRONMENT" ]; then
	"$E2E_DIR/external.sh" down
	read_lab
	if [ -s "$OUT_DIR/hosted-port-forward.pid" ]; then
		kill "$(cat "$OUT_DIR/hosted-port-forward.pid")" 2>/dev/null || true
	fi
	DOCKER_CONFIG=$E2E_DOCKER_CONFIG docker --context "$E2E_DOCKER_CONTEXT" image rm --force ptah-console:e2e \
		>/dev/null 2>&1 || true
fi
if [ -d "$OPERATOR_DIR/.git" ]; then
	(cd "$OPERATOR_DIR" && make demo-down)
fi
rm -rf "$OUT_DIR"
