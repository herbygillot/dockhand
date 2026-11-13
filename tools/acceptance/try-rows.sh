#!/usr/bin/env bash
# try-rows.sh runs the rows a commit adds or changes once against this
# checkout's build, in the quick stage's scratch world, before the commit
# lands: no row's first real run is on a candidate (#227). In v0.8.0-rc4,
# G33, G36, G38 and D-T1 failed on their own logic the first time they met
# a real dockhand, though selftest had walked each against synthetic logs.
#
#   tools/acceptance/try-rows.sh [--base <rev>] [--rows "G36 G38"]
#
# The rows are those whose rows/<ID>.sh differs from --base, origin/main
# unless given, or the ones --rows names. A row the quick stage runs runs
# there, under tools/heavy, and anything but a pass, a good refusal, or a
# known issue fails this, "not run" and "not exercised" included. A row
# only the full stage runs is walked to its first step for the test host
# (quick.sh --dry-full), which is as far as a scratch world reaches; it's
# listed, so its first host run is watched.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
base=origin/main
rows=""
while [ $# -gt 0 ]; do
	case "$1" in
	--base) base=$2; shift 2 ;;
	--rows) rows=$(printf '%s' "$2" | tr ',' ' '); shift 2 ;;
	-h | --help) sed -n '2,17p' "$0"; exit 0 ;;
	*) echo "try-rows.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done
if [ -z "$rows" ]; then
	rows=$({ git -C "$repo" diff --name-only "$base" -- tools/acceptance/rows; git -C "$repo" ls-files --others --exclude-standard -- tools/acceptance/rows; } |
		sed -n 's|^tools/acceptance/rows/\(.*\)\.sh$|\1|p' | sort -uV | tr '\n' ' ')
fi
rows=${rows% }
if [ -z "$rows" ]; then
	echo "try-rows.sh: no row differs from $base"
	exit 0
fi
quick="" full=""
for row in $rows; do
	file="$here/rows/$row.sh"
	[ -f "$file" ] || { echo "try-rows.sh: $row is gone; nothing to run"; continue; }
	if grep -qE '^# stages:(.* )?quick( |$)' "$file"; then
		quick="$quick $row"
	else
		full="$full $row"
	fi
done
status=0
candidate="try-$(git -C "$repo" rev-parse --short HEAD)"
state=${ACCEPT_STATE:-$HOME/.dockhand-acceptance/quick}
if [ -n "$quick" ]; then
	echo "try-rows.sh: running${quick} in the quick stage"
	"$repo/tools/heavy" "$here/quick.sh" --candidate "$candidate" --rows "${quick# }" || :
	for row in $quick; do
		result=$(jq -r '.result' "$state/results/$candidate/$row.json" 2>/dev/null || echo "no result")
		case "$result" in
		pass | "refused well" | "known issue") echo "try-rows.sh: $row $result" ;;
		*)
			echo "try-rows.sh: $row $result: $(jq -r '.why' "$state/results/$candidate/$row.json" 2>/dev/null)"
			status=1
			;;
		esac
	done
fi
if [ -n "$full" ]; then
	echo "try-rows.sh: walking${full}, full stage only, to its first step for the test host"
	"$here/quick.sh" --dry-full --candidate "$candidate-dry" --rows "${full# }" || :
	for row in $full; do
		echo "try-rows.sh: $row $(jq -r '.result + ": " + .why' "$state/results/$candidate-dry/$row.json" 2>/dev/null || echo "no result"); its first host run is on the test host"
	done
fi
exit "$status"
