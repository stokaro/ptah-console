#!/bin/sh
# Joins the acceptance rows' recordings into one film, in the order the rows
# ran, with the waits each row marked cut out: the operator applying a plan
# takes minutes, and a film of it would be mostly an unchanged page.
#
#   RESULTS  Playwright's output directory (default web/test-results)
#   FILM     the film to write (default .e2e/film/ptah-console-demo.mp4)
set -eu
ROOT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
RESULTS=${RESULTS:-$ROOT_DIR/web/test-results}
FILM=${FILM:-$ROOT_DIR/.e2e/film/ptah-console-demo.mp4}
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$(dirname "$FILM")"

find "$RESULTS" -name film.json | while read -r film; do
	printf '%s %s\n' "$(jq -r .startedAt "$film")" "$(dirname "$film")"
done | sort -n | while read -r _ dir; do
	[ -f "$dir/video.webm" ] || continue
	count=$(($(wc -l <"$WORK/list" 2>/dev/null || echo 0) + 1))
	# A second and a half stays on either side of a wait, so a cut reads as
	# a cut rather than as a page that jumped.
	waits=$(jq -r '[.cuts[] | select(.to - .from > 4) | "between(t,\(.from + 1.5),\(.to - 1.5))"] | join("+")' \
		"$dir/film.json")
	filter="fps=25"
	if [ -n "$waits" ]; then
		filter="$filter,select='not($waits)',setpts=N/(25*TB)"
	fi
	ffmpeg -nostdin -loglevel error -y -i "$dir/video.webm" -vf "$filter" -an \
		-c:v libx264 -pix_fmt yuv420p -r 25 "$WORK/$count.mp4"
	printf "file '%s'\n" "$WORK/$count.mp4" >>"$WORK/list"
done
[ -s "$WORK/list" ] || {
	printf 'film: no recording under %s\n' "$RESULTS" >&2
	exit 1
}
ffmpeg -nostdin -loglevel error -y -f concat -safe 0 -i "$WORK/list" -c copy -movflags +faststart "$FILM"
printf 'film: %s rows in %s\n' "$(wc -l <"$WORK/list" | tr -d ' ')" "$FILM"
