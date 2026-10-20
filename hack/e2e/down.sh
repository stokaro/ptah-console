#!/bin/sh
# Removes the acceptance environment: the operator's lab, with every container
# and volume it created, and the kubeconfigs this repository wrote.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

if [ -d "$OPERATOR_DIR/.git" ]; then
	(cd "$OPERATOR_DIR" && make demo-down)
fi
rm -rf "$OUT_DIR"
