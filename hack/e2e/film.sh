#!/bin/sh
# Joins the acceptance rows' recordings into one film, in the order the rows
# ran, with the waits each row marked cut out: the operator applying a plan
# takes minutes, and a film of it would be mostly an unchanged page.
#
#   RESULTS  Playwright's output directory (default web/test-results)
#   FILM     the film to write (default .e2e/film/ptah-console-demo.mp4)
#   ROWS     the row files to take, such as "acceptance.spec.ts
#            console.spec.ts"; empty takes every row that was recorded
set -eu
ROOT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
RESULTS=${RESULTS:-$ROOT_DIR/web/test-results}
FILM=${FILM:-$ROOT_DIR/.e2e/film/ptah-console-demo.mp4}
ROWS=${ROWS:-}
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$(dirname "$FILM")"
: >"$WORK/list"

find "$RESULTS" -name film.json | while read -r film; do
	file=$(jq -r '.file // ""' "$film")
	case " $ROWS " in
	"  " | *" $file "*) ;;
	*) continue ;;
	esac
	printf '%s %s\n' "$(jq -r .startedAt "$film")" "$(dirname "$film")"
done | sort -n | while read -r _ dir; do
	[ -f "$dir/video.webm" ] || continue
	count=$(($(wc -l <"$WORK/list") + 1))
	# Each cut says how much of its edges to keep: a second and a half of a
	# wait, so the cut reads as a cut rather than as a page that jumped, and
	# none of a sign-in, which shows a blank page.
	waits=$(jq -r '[.cuts[] | select(.to - .from > 2 * .keep + 0.5) | "between(t,\(.from + .keep),\(.to - .keep))"] | join("+")' \
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
	printf 'film: no recording of %s under %s\n' "${ROWS:-any row}" "$RESULTS" >&2
	exit 1
}
ffmpeg -nostdin -loglevel error -y -f concat -safe 0 -i "$WORK/list" -c copy -movflags +faststart "$FILM"
printf 'film: %s rows in %s\n' "$(wc -l <"$WORK/list" | tr -d ' ')" "$FILM"
