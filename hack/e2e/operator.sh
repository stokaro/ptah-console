#!/bin/sh
# Clones ptah-operator into DIR at the commit go.mod requires and prints that
# commit. The acceptance rows run against the operator the console is built
# against, not whatever master holds.
#
#   hack/e2e/operator.sh DIR           clone into DIR
#   hack/e2e/operator.sh --check DIR   refuse a clone in DIR at another commit
#
# A released version is checked out by its tag and a pseudo-version by the
# commit it names. Any other version is refused rather than guessed at.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

check=false
if [ "${1:-}" = --check ]; then
	check=true
	shift
fi
[ $# -eq 1 ] || fail "usage: hack/e2e/operator.sh [--check] DIR"
dir=$1

version=$(cd "$ROOT_DIR" && go list -m -f '{{.Version}}' github.com/stokaro/ptah-operator)
if printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	revision="refs/tags/$version"
elif printf '%s\n' "$version" | grep -Eq '[.-][0-9]{14}-[0-9a-f]{12}$'; then
	revision=${version##*-}
else
	fail "cannot read a commit from $version"
fi

if $check; then
	# A lab stays up across runs, and the clone it was built from with it; a
	# go.mod that moved since would otherwise run the rows against the
	# operator it left.
	want=$(git -C "$dir" rev-parse --verify --quiet "$revision^{commit}" || true)
	have=$(git -C "$dir" rev-parse HEAD)
	[ -n "$want" ] && [ "$want" = "$have" ] ||
		fail "the operator in $dir is at $have, and go.mod requires $version; run make e2e-down and remove $dir"
	exit 0
fi

say "checking out the operator $version at $revision" >&2
git clone --quiet --filter=blob:none https://github.com/stokaro/ptah-operator.git "$dir"
git -C "$dir" checkout --quiet --detach "$revision"
git -C "$dir" rev-parse HEAD
