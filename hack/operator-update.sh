#!/usr/bin/env bash
# Keeps one pull request that moves the console to the newest released
# stokaro/ptah-operator, the way docs/releases.md moves it by hand, so CI shows
# what a new operator release breaks before anyone starts a console release.
#
# .github/workflows/operator-update.yml runs it every day. It changes nothing
# while main requires the newest release, or while the branch already names it.
# It rebuilds the branch from main only while every commit on the branch is
# its own; once a person has pushed there, it leaves the branch alone and says
# on the pull request that a newer release is out. A branch that names the
# newest release with no pull request is rebuilt and proposed again, unless
# a closed pull request already had its head.
#
# GH_TOKEN is the ptah-automation App's token, so the pull request it opens
# and each push to it start the pull request checks, which a push or a pull
# request made with the workflow's own token would not. RELEASES_TOKEN, when
# set, reads the operator's releases, which the App's token for this
# repository alone may not reach.
#
# DRY_RUN=1 builds the branch locally and pushes, opens and comments nothing.
set -euo pipefail

branch="${OPERATOR_UPDATE_BRANCH:-deps/ptah-operator}"
module=github.com/stokaro/ptah-operator
bot_name='ptah-automation[bot]'
bot_email='321450905+ptah-automation[bot]@users.noreply.github.com'
repository="${GITHUB_REPOSITORY:-stokaro/ptah-console}"
dry_run="${DRY_RUN:-}"

notice() { printf '%s\n' "$*"; [ -z "${GITHUB_STEP_SUMMARY:-}" ] || printf '%s\n\n' "$*" >>"$GITHUB_STEP_SUMMARY"; }
# code lists its arguments as Markdown code, comma-separated.
code() {
  local item out=""
  for item in "$@"; do out+="\`$item\`, "; done
  printf '%s' "${out%, }"
}

git fetch --quiet --tags origin main
current="$(git show origin/main:go.mod | awk -v m="$module" '$1 == m { print $2 }')"
latest="$(GH_TOKEN="${RELEASES_TOKEN:-${GH_TOKEN:-}}" gh api --paginate "repos/stokaro/ptah-operator/releases" \
  --jq '.[] | select(.draft == false and .prerelease == false) | .tag_name' |
  grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n 1)"
if [ -z "$current" ] || [ -z "$latest" ]; then
  echo "operator-update: main requires '$current' and the newest release is '$latest'" >&2
  exit 1
fi
# A pseudo-version such as v0.2.17-0.20261121000000-abcdef012345 is a commit
# before v0.2.17, so v0.2.17 moves it; sort -V alone would put it after.
base="${current%%-*}"
if [ "$(printf '%s\n%s\n' "$base" "$latest" | sort -V | tail -n 1)" != "$latest" ] ||
  { [ "$base" = "$current" ] && [ "$latest" = "$current" ]; }; then
  notice "main requires $module $current, and the newest release is $latest: nothing to move."
  exit 0
fi

pr="$(gh pr list --repo "$repository" --head "$branch" --state open --json number --jq '.[0].number // empty')"
expected=""
if git fetch --quiet origin "refs/heads/$branch"; then
  expected="$(git rev-parse FETCH_HEAD)"
  on_branch="$(git show FETCH_HEAD:go.mod | awk -v m="$module" '$1 == m { print $2 }')"
  if [ "$on_branch" = "$latest" ]; then
    if [ -n "$pr" ]; then
      notice "$branch already moves to $latest in #$pr: nothing to do."
      exit 0
    fi
    # A closed pull request with this head is one someone declined; a branch
    # no pull request ever had is a run that pushed and then failed to open
    # one, which the rebuild below finishes.
    if gh pr list --repo "$repository" --head "$branch" --state closed --json headRefOid --jq '.[].headRefOid' |
      grep -q -x -F "$expected"; then
      notice "$branch moves to $latest, and its pull request was closed: nothing to do until a newer release."
      exit 0
    fi
  fi
  others="$(git log --format='%ae' "origin/main..$expected" | grep -v -x -F "$bot_email" || true)"
  if [ -n "$others" ]; then
    if [ -z "$pr" ]; then
      notice "::warning::$branch holds commits this workflow did not make and no open pull request; it is left alone until someone deletes or merges it."
      exit 0
    fi
    marker="<!-- operator-update: $latest -->"
    if gh pr view "$pr" --repo "$repository" --json comments --jq '.comments[].body' | grep -q -F "$marker"; then
      notice "#$pr already says $latest is released."
      exit 0
    fi
    body="$module $latest is released. This pull request has commits this workflow did not make, so it does not rebuild the branch. Move it with \`go get $module@$latest\` and the steps in docs/releases.md, or close the pull request and delete the branch, and the workflow opens a new one.

$marker"
    if [ -n "$dry_run" ]; then
      notice "would comment on #$pr: $body"
    else
      gh pr comment "$pr" --repo "$repository" --body "$body"
      notice "commented on #$pr that $latest is released."
    fi
    exit 0
  fi
fi

git switch --quiet -C "$branch" origin/main
failed=()
step() {
  local name="$1"
  shift
  if ! "$@"; then
    failed+=("$name")
  fi
}
go get "$module@$latest"
step "go mod tidy" go mod tidy
step "make web-install" make web-install
step "make generate" make generate
step "go run ./hack/releasecheck -move" go run ./hack/releasecheck -move
step "go test ./test/chart -update" go test ./test/chart -update
console="$(awk '$1 == "version:" { print $2; exit }' charts/ptah-console/Chart.yaml)"

if [ "${#failed[@]}" -eq 0 ]; then
  failures="Every step of the move ran."
else
  failures="These steps of the move failed, and what they would have written is missing: $(code "${failed[@]}"). The workflow run's log has their output."
fi

git add -A
git -c user.name="$bot_name" -c user.email="$bot_email" commit --quiet \
  -m "Move to operator $latest" \
  -m "$module $latest is released, and main requires $current. This moves the console to it as docs/releases.md does: go get and go mod tidy, make generate, a support/compatibility.json row for console $console, both charts at $console, and the chart test goldens. $failures"

title="Move to operator $latest"
body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT
cat >"$body_file" <<EOF
## Summary

Ptah Operator $latest is released, and the console still builds against $current. This pull request moves the console to $latest the way the release guide moves it by hand, so CI shows what the new operator release breaks before anyone starts a console release.

## Goal and motivation

The console reads the operator's API types and plan reader as a Go module, and a console release names one released operator. The Operator update workflow opened it on its own, as it does each time a newer operator release is out.

## Expected outcome

- \`go.mod\` requires $module $latest, and \`go mod tidy\` ran.
- \`make generate\` copied the operator's example roles into both charts.
- \`support/compatibility.json\` and docs/compatibility.md name console $console with operator $latest and its Kubernetes window, and both charts are at $console. The person who releases may choose another version.
- The chart test goldens are written again for that version.

$failures

## Risks and compatibility

Nothing is released by this pull request. Before v1 the operator promises no API compatibility, so a red check here is what this pull request exists to show.

The workflow rebuilds this branch from main for a newer operator release only while every commit on it is the workflow's own. Once someone pushes here, it leaves the branch alone and comments when a newer release is out.

## Verification

The checks of this pull request are its verification. The move itself runs only the chart test, to write its goldens again.
EOF

if [ -n "$dry_run" ]; then
  notice "dry run: built $branch for $latest at $(git rev-parse --short HEAD); failed steps: ${failed[*]:-none}"
  exit 0
fi

# An empty expected value requires that the branch does not exist yet.
git push --quiet --force-with-lease="refs/heads/$branch:$expected" origin "HEAD:refs/heads/$branch"
if [ -n "$pr" ]; then
  gh pr edit "$pr" --repo "$repository" --title "$title" --body-file "$body_file"
  notice "#$pr moves to $latest."
else
  gh pr create --repo "$repository" --base main --head "$branch" --title "$title" --body-file "$body_file"
  notice "opened a pull request that moves to $latest."
fi
