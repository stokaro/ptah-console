#!/usr/bin/env sh
# Runs teststyle over the test files git tracks, modeled on stokaro/ptah's
# scripts/check-test-style.sh.
#
# The scanner is never handed the repository to walk: it prunes by directory
# name only, and a linked worktree parked under the checkout, or a node_modules
# tree under web/, would be scanned as if it were this repository's code. git
# is the authority on what belongs to this checkout.
set -eu

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/ptah-console-teststyle.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT INT TERM
scan_root="$work_dir/root"
path_list="$work_dir/paths"
mkdir -p "$scan_root"

# Tracked files plus new ones not yet added, so the gate fires before
# `git add`. Materialized first: a pipe would hide a failure of git itself.
git -c core.quotePath=false ls-files --cached --others --exclude-standard -- '*_test.go' >"$path_list"
if [ ! -s "$path_list" ]; then
	echo "teststyle: no test files selected from git; refusing to report success" >&2
	exit 1
fi

count=0
while IFS= read -r path; do
	if [ ! -f "$path" ]; then
		echo "teststyle: skipping $path (not a regular file in the working tree)" >&2
		continue
	fi
	mkdir -p "$scan_root/$(dirname "$path")"
	cp "$path" "$scan_root/$path"
	count=$((count + 1))
done <"$path_list"
if [ "$count" -eq 0 ]; then
	echo "teststyle: no test file was copied; refusing to report success" >&2
	exit 1
fi

# No baseline: the repository starts clean and stays that way. The empty file
# is written outside the tree so a stray baseline cannot excuse a finding.
printf '{}\n' >"$work_dir/baseline.json"
GOWORK=off go tool teststyle -skip-examples -baseline "$work_dir/baseline.json" -root "$scan_root"
echo "teststyle: $count test files checked"
