#!/usr/bin/env bash
# run.sh runs acceptance rows and the harm sweep after each, writing
# results/<candidate>/<row>.json (prime-time.md; the project's
# plan/acceptance-harness.md, H1).
#
#   tools/acceptance/run.sh --stage quick|full --candidate <rc> [--rows "A3 B1"]
#
# A row is rows/<ID>.sh, which says the stages it runs in on a line
# "# stages: quick full" and defines setup, act, and assert. The runner
# snapshots between setup and act and after act, and the harm sweep
# compares them: a row any invariant breaks is a blocker, whatever it
# said of itself. It runs in the stage's scratch environment, ACCEPT_STATE,
# never the person's own.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
stage=quick
candidate=""
rows=""
rowdir="$here/rows"
while [ $# -gt 0 ]; do
	case "$1" in
	--stage) stage=$2; shift 2 ;;
	--candidate) candidate=$2; shift 2 ;;
	--rows) rows=$(printf '%s' "$2" | tr ',' ' '); shift 2 ;;
	--row-dir) rowdir=$2; shift 2 ;;
	-h | --help) sed -n '2,13p' "$0"; exit 0 ;;
	*) echo "run.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done
case "$stage" in quick | full) ;; *) echo "run.sh: --stage is quick or full" >&2; exit 2 ;; esac
if [ -z "${ACCEPT_STATE:-}" ] || [ ! -d "$ACCEPT_STATE" ]; then
	echo "run.sh: ACCEPT_STATE names no directory; the stage's environment sets it, so a run never touches your own state" >&2
	exit 2
fi
# shellcheck source=lib/guard.sh
. "$here/lib/guard.sh"
guard "$stage" || exit 2
[ -n "$candidate" ] || candidate=$(git -C "$here" describe --tags --always --dirty 2>/dev/null || echo dev)
export ACCEPT_STAGE=$stage ACCEPT_CANDIDATE=$candidate

# The rows of this stage, in order, unless --rows names them.
if [ -z "$rows" ]; then
	for file in "$rowdir"/*.sh; do
		[ -f "$file" ] || continue
		grep -qE "^# stages:(.* )?$stage( |\$)" "$file" && rows="$rows $(basename "$file" .sh)"
	done
fi

results="$ACCEPT_STATE/results/$candidate"
mkdir -p "$results"
failed=0
printf '%-8s %-14s %s\n' ROW RESULT WHY
for row in $rows; do
	file="$rowdir/$row.sh"
	if [ ! -f "$file" ]; then
		echo "run.sh: no row $row" >&2
		exit 2
	fi
	ROW_DIR="$results/$row"
	rm -rf "$ROW_DIR"
	mkdir -p "$ROW_DIR/json"
	: >"$ROW_DIR/out.log"
	export ROW_DIR ROW_ID=$row
	(
		set +e
		# shellcheck source=lib/common.sh
		. "$here/lib/common.sh"
		# shellcheck source=lib/harm.sh
		. "$here/lib/harm.sh"
		setup() { :; }
		act() { :; }
		assert() { :; }
		# shellcheck disable=SC1090
		. "$file"
		if ! setup; then
			row_fail "its setup failed"
		fi
		harm_snapshot "$ROW_DIR/before"
		[ -f "$ROW_DIR/result" ] || act || :
		harm_snapshot "$ROW_DIR/after"
		[ -f "$ROW_DIR/result" ] || assert || :
		harm_check
	) >"$ROW_DIR/runner.log" 2>&1 || :

	result=$(cat "$ROW_DIR/result" 2>/dev/null || echo fail)
	why=$(cat "$ROW_DIR/why" 2>/dev/null || echo "the row gave no result")
	[ -f "$ROW_DIR/result" ] || why="the row gave no result"
	harmful=""
	for verdict in "$ROW_DIR"/harm/H*; do
		[ -f "$verdict" ] || continue
		case "$(cat "$verdict")" in broken*) harmful="$harmful $(basename "$verdict")" ;; esac
	done
	if [ -n "$harmful" ]; then
		result=blocker
		why="harm:$harmful${why:+; $why}"
	fi
	jq -n --arg row "$row" --arg stage "$stage" --arg candidate "$candidate" --arg result "$result" --arg why "$why" \
		--arg log "$ROW_DIR/out.log" --argjson harm "$(for v in "$ROW_DIR"/harm/H*; do [ -f "$v" ] && jq -n --arg k "$(basename "$v")" --arg v "$(cat "$v")" '{($k): $v}'; done | jq -s 'add // {}')" \
		--argjson exits "$(for e in "$ROW_DIR"/json/*.json.exit; do [ -f "$e" ] && cat "$e"; done | jq -s '.')" \
		'{row: $row, stage: $stage, candidate: $candidate, result: $result, why: $why, harm: $harm, exit_codes: $exits, log: $log}' >"$results/$row.json"
	printf '%-8s %-14s %s\n' "$row" "$result" "$why"
	case "$result" in pass | "refused well" | "known issue") ;; *) failed=$((failed + 1)) ;; esac
done
echo "Results: $results"
[ "$failed" -eq 0 ]
