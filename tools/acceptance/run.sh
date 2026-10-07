#!/usr/bin/env bash
# run.sh runs acceptance rows and the harm sweep after each, writing
# results/<candidate>/<row>.json (prime-time.md; the project's
# plan/acceptance-harness.md, H1).
#
#   tools/acceptance/run.sh --stage quick|full --candidate <rc> [--rows "A3 B1" | --rows-file <file>] [--dry-run]
#
# --rows-file names the rows in a file, one ID a line, blank lines and
# lines from # on aside, as a rerun list (rerun-rc7): run in their IDs'
# order, as --rows's, and refused whole where one names no row.
#
# --dry-run walks the full stage's rows in the quick stage's scratch
# environment, each up to its first step that needs the test host or a
# person, where it stops as "not run" (lib/protocol.sh).
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
	--rows-file)
		[ -f "$2" ] || { echo "run.sh: --rows-file $2 is no file" >&2; exit 2; }
		rows=$(sed -e 's/#.*//' -e 's/[[:space:]]//g' "$2" | { grep -v '^$' || :; } | sort -V | tr '\n' ' ')
		[ -n "$rows" ] || { echo "run.sh: --rows-file $2 names no rows" >&2; exit 2; }
		shift 2
		;;
	--row-dir) rowdir=$2; shift 2 ;;
	--dry-run) ACCEPT_DRY=1; shift ;;
	-h | --help) sed -n '2,21p' "$0"; exit 0 ;;
	*) echo "run.sh: unknown argument $1" >&2; exit 2 ;;
	esac
done
case "$stage" in quick | full) ;; *) echo "run.sh: --stage is quick or full" >&2; exit 2 ;; esac
for id in $rows; do
	[ -f "$rowdir/$id.sh" ] || { echo "run.sh: no row $id in $rowdir" >&2; exit 2; }
done
if [ -z "${ACCEPT_STATE:-}" ] || [ ! -d "$ACCEPT_STATE" ]; then
	echo "run.sh: ACCEPT_STATE names no directory; the stage's environment sets it, so a run never touches your own state" >&2
	exit 2
fi
# shellcheck source=lib/guard.sh
. "$here/lib/guard.sh"
# A dry run takes no step of the host's, and is held to the quick stage's
# guard, since it runs in the quick stage's environment.
export ACCEPT_DRY=${ACCEPT_DRY:-0}
if [ "$ACCEPT_DRY" = 1 ]; then
	guard quick || exit 2
else
	guard "$stage" || exit 2
fi
[ -n "$candidate" ] || candidate=$(git -C "$here" describe --tags --match 'v*' --exact-match 2>/dev/null || git -C "$here" rev-parse --short HEAD 2>/dev/null || echo dev)
export ACCEPT_STAGE=$stage ACCEPT_CANDIDATE=$candidate
# fd 3 is the run's own output, which a row's checkpoint writes its
# WAITING to, past the row's runner.log (lib/protocol.sh).
exec 3>&1

# The rows of this stage, in order, unless --rows names them. A row
# marked "# order: alone", as S1's day of serve, runs only when named, so
# it holds up no other row; the run says how to run it after.
# Rows go in their IDs' numeric order, A2 before A10, as the plan lists
# them: in glob order A12's Xcode image ran before A2 made the base image
# it needs (the rc3 full run, 2026-10-06).
alone=""
if [ -z "$rows" ]; then
	for id in $(find "$rowdir" -maxdepth 1 -name '*.sh' -type f -exec basename {} .sh \; | sort -V); do
		file="$rowdir/$id.sh"
		grep -qE "^# stages:(.* )?$stage( |\$)" "$file" || continue
		if grep -q '^# order: alone' "$file"; then
			alone="$alone $(basename "$file" .sh)"
		else
			rows="$rows $(basename "$file" .sh)"
		fi
	done
fi

results="$ACCEPT_STATE/results/$candidate"
mkdir -p "$results"
failed=0
notrun=0

# The full stage's pull requests are approved before any row runs: the
# list goes to the person, who leaves the lines they approve.
# shellcheck source=lib/protocol.sh
. "$here/lib/protocol.sh"
if [ "$stage" = full ]; then
	# shellcheck disable=SC2086
	protocol_list_prs "$ACCEPT_STATE/prs.intended" "$rowdir" $rows
	if [ -s "$ACCEPT_STATE/prs.intended" ] && protocol_live; then
		echo "The pull requests the rows mean to open, row, port, and test or real:"
		sed 's/^/  /' "$ACCEPT_STATE/prs.intended"
		ROW_ID=approval ROW_DIR="$results/approval"
		mkdir -p "$ROW_DIR"
		say() { printf '%s\n' "$*"; }
		row_result() { :; }
		checkpoint "approve the pull requests in $ACCEPT_STATE/prs.intended: test ones go to the sandbox ${DOCKHAND_PULL_REQUESTS:-}; change test to real for the few that go to MacPorts, and delete a line to open nothing" ||
			: >"$ACCEPT_STATE/prs.intended"
		unset -f say row_result
	fi
fi
# isolate_branches sets aside, before a full stage's row, the open branches
# earlier rows left, so the row's port selects only its own: rows pick
# their branch by port, and A10's examples and B1's update left three of
# go-reflex's open, so B1's check -p was refused as ambiguous (the rc6
# full stage). A branch whose pull request is still open stays, for the
# rows that ask a person to name one, and is said. What's left open is
# written to the row's branches.before, which own_branch reads.
isolate_branches() {
	local open name
	if [ "$stage" != full ] || [ "${ACCEPT_DRY:-0}" = 1 ] || [ ! -x "${DH_BIN:-}" ]; then
		: >"$ROW_DIR/branches.before"
		return 0
	fi
	open=$("$DH_BIN" --json status 2>/dev/null | jq -r '.result.branches[]? | [.name, (.pull_request.state // "")] | @tsv' 2>/dev/null || :)
	while IFS=$'\t' read -r name state; do
		[ -n "$name" ] || continue
		# A row that runs in halves around a reboot, as D-I5, names the
		# branch its second half finds in $ACCEPT_STATE/keep-branches.
		if grep -qxF "$name" "$ACCEPT_STATE/keep-branches" 2>/dev/null; then
			printf 'kept %s: a row'"'"'s second half needs it\n' "$name" >>"$ROW_DIR/isolation.log"
			continue
		fi
		if [ -n "$state" ] && [ "$state" != closed ] && [ "$state" != merged ]; then
			printf 'kept %s: its pull request is %s\n' "$name" "$state" >>"$ROW_DIR/isolation.log"
			printf '%s: %s stays open, its pull request %s; a row selecting its port may find it\n' "$row" "$name" "$state" >&3
			continue
		fi
		printf '$ dockhand archive %s\n' "$name" >>"$ROW_DIR/isolation.log"
		"$DH_BIN" archive "$name" </dev/null >>"$ROW_DIR/isolation.log" 2>&1 || :
	done <<EOT
$open
EOT
	"$DH_BIN" --json status 2>/dev/null | jq -r '.result.branches[]?.name' >"$ROW_DIR/branches.before" 2>/dev/null || :
	# Remote-tracking refs of fork branches gone go before the row, outside
	# its harm sweep's window, so no row's prune finds them (the rc6 full
	# stage, D-S9).
	git -C "${MACPORTS_TREE:?}" remote prune origin >>"$ROW_DIR/isolation.log" 2>&1 || :
}

printf '%-8s %-14s %s\n' ROW RESULT WHY
for row in $rows; do
	file="$rowdir/$row.sh"
	if [ ! -f "$file" ]; then
		echo "run.sh: no row $row" >&2
		exit 2
	fi
	ROW_DIR="$results/$row"
	rm -rf "$ROW_DIR"
	# A row starts from the stage's environment as made, where the stage
	# says how to make it again.
	if [ -n "${ACCEPT_RESET:-}" ]; then
		# shellcheck disable=SC2086
		$ACCEPT_RESET || { echo "run.sh: the stage's environment couldn't be made again for $row" >&2; exit 1; }
	fi
	mkdir -p "$ROW_DIR/json"
	: >"$ROW_DIR/out.log"
	# Each dockhand the row runs, H6's included, says in this file what it
	# sent GitHub's API and what GitHub last said was spent of the hour's
	# allowance: GitHub's rate_limit endpoint said nothing was spent for a
	# fine-grained token whose calls were refused (the M1's run at
	# 1da4fdbf).
	export DOCKHAND_GITHUB_LOG="$ROW_DIR/github.log"
	: >"$DOCKHAND_GITHUB_LOG"
	export ROW_DIR ROW_ID=$row ROW_LIB="$here/lib"
	isolate_branches
	(
		set +e
		# shellcheck source=lib/common.sh
		. "$here/lib/common.sh"
		# shellcheck source=lib/harm.sh
		. "$here/lib/harm.sh"
		# shellcheck source=lib/protocol.sh
		. "$here/lib/protocol.sh"
		setup() { :; }
		act() { :; }
		assert() { :; }
		teardown() { :; }
		# shellcheck disable=SC1090
		. "$file"
		if ! setup; then
			row_fail "its setup failed"
		fi
		harm_snapshot "$ROW_DIR/before"
		[ -f "$ROW_DIR/result" ] || act || :
		settle_stopped || :
		harm_snapshot "$ROW_DIR/after"
		[ -f "$ROW_DIR/result" ] || assert || :
		# A row's teardown runs whatever came before it, its setup's
		# failure included, and H7 reads what it leaves (the M1's run at
		# 1da4fdbf: D-R2's setup failed with both slot VMs started).
		teardown || :
		harm_running >"$ROW_DIR/after/running.teardown" 2>/dev/null || :
		harm_check
	) >"$ROW_DIR/runner.log" 2>&1 || :

	spent=$(awk -F '\t' '{n += $1} END {print n + 0}' "$DOCKHAND_GITHUB_LOG" 2>/dev/null)
	used=$(awk -F '\t' '$2 > m {m = $2} END {print m + 0}' "$DOCKHAND_GITHUB_LOG" 2>/dev/null)
	result=$(cat "$ROW_DIR/result" 2>/dev/null || echo fail)
	why=$(cat "$ROW_DIR/why" 2>/dev/null || echo "the row gave no result")
	[ -f "$ROW_DIR/result" ] || why="the row gave no result"
	# GitHub's rate limit is the stage's want, never a good refusal.
	if [ "$result" = "refused well" ] && grep -qi "rate limit" "$ROW_DIR/out.log" 2>/dev/null; then
		result=fail
		why="GitHub's rate limit, not a refusal: $(grep -i -m1 "rate limit" "$ROW_DIR/out.log" | cut -c1-200)"
	fi
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
		--argjson spent "${spent:-0}" --argjson used "${used:-0}" \
		'{row: $row, stage: $stage, candidate: $candidate, result: $result, why: $why, harm: $harm, exit_codes: $exits, log: $log,
		  github_requests: $spent, github_used_after: $used}' >"$results/$row.json"
	printf '%-8s %-14s %s\n' "$row" "$result" "$why"
	case "$result" in
	pass | "refused well" | "known issue") ;;
	"not run") notrun=$((notrun + 1)) ;;
	*) failed=$((failed + 1)) ;;
	esac
done
[ "$notrun" -eq 0 ] || echo "$notrun not run: each stopped at a step for the host or a person, as its row says"
for row in $alone; do
	echo "$row runs alone, after this run: run it with --rows $row"
done
echo "Results: $results"
[ "$failed" -eq 0 ]
