# shellcheck shell=bash
# The row API: what a row script calls to run dockhand, say what it may
# change, and give its result. A row defines setup, act, and assert; the
# runner snapshots for the harm sweep between setup and act, and again
# after act. macOS's /bin/bash is 3.2, so nothing here needs a newer one.

# DH_BIN is the dockhand under test; ROW_DIR is the row's own directory
# in the results, set by the runner.
: "${DH_BIN:=dockhand}"

# dh runs dockhand as a person would, its output kept for the harm sweep
# (H4's token grep, H6's Next: lines), and returns its exit status.
dh() {
	local status=0
	printf '$ dockhand %s\n' "$*" >>"$ROW_DIR/out.log"
	"$DH_BIN" "$@" >>"$ROW_DIR/out.log" 2>&1 || status=$?
	printf '[exit %d]\n' "$status" >>"$ROW_DIR/out.log"
	return "$status"
}

# next_superseded marks the Next: lines printed so far as undone by the
# row's own doing, as a rename or a deletion it makes on purpose: H6 runs
# only the Next: lines after the last mark (the M1's rerun, D-S5, D-S9).
next_superseded() { printf '# Next: lines above were superseded by the row: %s\n' "$*" >>"$ROW_DIR/out.log"; }

# dh_setup runs a step that only sets the row up, such as the update
# that makes a branch for it: where it fails, the row fails as its setup's,
# with what dockhand said, rather than as a later step's that never ran.
dh_setup() {
	local status=0 mark
	mark=$(wc -l <"$ROW_DIR/out.log")
	dh "$@" || status=$?
	if [ "$status" -ne 0 ]; then
		row_fail "its setup, dockhand $*, exited $status: $(tail -n +"$((mark + 2))" "$ROW_DIR/out.log" | grep -v '^\[exit' | tail -2 | tr '\n' ' ')"
		return "$status"
	fi
}

# dh_json runs dockhand with --json once, keeping the envelope and the
# process's exit status for H8, and returns that status; DH_LAST_JSON
# names the envelope's file. Its output is also in out.log, for H4.
dh_json() {
	local status=0 n
	n=$(find "$ROW_DIR/json" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
	local file="$ROW_DIR/json/$((n + 1)).json"
	printf '$ dockhand --json %s\n' "$*" >>"$ROW_DIR/out.log"
	"$DH_BIN" --json "$@" >"$file" 2>>"$ROW_DIR/out.log" || status=$?
	cat "$file" >>"$ROW_DIR/out.log"
	printf '%s\n' "$status" >"$file.exit"
	printf '%s\n' "$*" >"$file.args"
	DH_LAST_JSON=$file
	return "$status"
}

# expect_exit runs a command and fails the row unless it exits as said.
expect_exit() {
	local want=$1 status=0
	shift
	"$@" || status=$?
	if [ "$status" -ne "$want" ]; then
		row_fail "$* exited $status, not $want"
		return 1
	fi
}

# The allowances: what a row is meant to change, which the harm sweep
# then doesn't count. Each is a glob.
allow_change() { printf '%s\n' "$@" >>"$ROW_DIR/allow.change"; }
allow_ref_gone() { printf '%s\n' "$@" >>"$ROW_DIR/allow.refgone"; }
allow_push() { printf '%s\n' "$@" >>"$ROW_DIR/allow.push"; }
allow_prs() { printf '%s\n' "$1" >"$ROW_DIR/allow.prs"; }
allow_running() { printf '%s\n' "$@" >>"$ROW_DIR/allow.running"; }

# A row's result, prime-time.md's: pass, refused well, known issue, or
# fail; the harm sweep makes a blocker of any. The first said stands.
row_result() {
	[ -f "$ROW_DIR/result" ] && return 0
	printf '%s\n' "$1" >"$ROW_DIR/result"
	printf '%s\n' "${2:-}" >"$ROW_DIR/why"
}
row_pass() { row_result pass "${1:-}"; }
row_refused_well() { row_result "refused well" "$1"; }
row_known() { row_result "known issue" "$1"; }
row_fail() { row_result fail "$1"; }

# say writes to the runner's own output, not the row's log.
say() { printf '%s\n' "$*" >&2; }

# with_timeout runs a command, killing it after some seconds: a command
# that would hang reads as exit 142 rather than holding the run. It sends
# TERM at the deadline, and KILL five seconds on, from a parent that
# waits: exec'ing the command under an alarm left it to the command,
# and Go ignores SIGALRM, so a dockhand check waiting on the VM limit ran
# 2h47m past its 180 seconds (the M1's run at 10aac0c3, D-R2).
with_timeout() {
	local seconds=$1 status=0
	shift
	perl -e '
		my $seconds = shift;
		my $pid = fork();
		die "fork: $!" unless defined $pid;
		if ($pid == 0) { exec { $ARGV[0] } @ARGV or exit 127; }
		$SIG{ALRM} = sub {
			kill "TERM", $pid;
			for (1 .. 5) { exit 142 if waitpid($pid, 1) == $pid; sleep 1; }
			kill "KILL", $pid;
			waitpid($pid, 0);
			exit 142;
		};
		alarm $seconds;
		waitpid($pid, 0);
		alarm 0;
		exit(($? & 127) ? 128 + ($? & 127) : $? >> 8);
	' "$seconds" "$@" || status=$?
	# A cut command is said where the runner reads it: what it left
	# stopped is the cut's, not dockhand's (settle_stopped).
	if [ "$status" = 142 ] && [ -n "${ROW_DIR:-}" ]; then
		printf '%s, after %ss\n' "$*" "$seconds" >>"$ROW_DIR/cut"
	fi
	return "$status"
}

# cut_command records a command a guard of the row's, rather than
# with_timeout, stopped before it finished, as one stopping serve while a
# toolchain still builds does.
cut_command() { printf '%s\n' "$*" >>"$ROW_DIR/cut"; }

# settle_stopped ends the runs a row left recorded as running with no
# live process behind them, so H7 reads a clean queue (the M1's run at
# d302e744: B3's serve was stopped mid-check, and its two runs read as
# left running). Each is listed in the row's notes and canceled. Where a
# cut stopped the process, the row isn't run, with what was cut;
# otherwise a run left so is the row's failure.
settle_stopped() {
	local runs name why
	# One stopped before the row began is an earlier row's, not this one's;
	# one a cut serve left queued, not yet taken, is the cut's too (the
	# M1's run at 11491d4e: B3's check-2).
	local queued=false
	[ -s "$ROW_DIR/cut" ] && queued=true
	runs=$("$DH_BIN" --json queue 2>/dev/null | jq -r --argjson queued "$queued" '.result.runs[]? | select(.stopped or ($queued and .state == "queued")) | .name' 2>/dev/null |
		while IFS= read -r name; do grep -qxF "run $name" "$ROW_DIR/before/running" 2>/dev/null || printf '%s\n' "$name"; done)
	[ -n "$runs" ] || return 0
	for name in $runs; do
		printf 'left stopped or queued, then canceled by the runner: %s\n' "$name" >>"$ROW_DIR/notes"
		"$DH_BIN" cancel "$name" >>"$ROW_DIR/out.log" 2>&1 || :
	done
	why="left $(printf '%s' "$runs" | tr '\n' ' ' | sed 's/ $//') stopped or queued, canceled before H7 read the queue"
	if [ -s "$ROW_DIR/cut" ]; then
		row_result "not run" "the runner cut $(paste -sd ';' "$ROW_DIR/cut"), which $why"
	else
		row_fail "dockhand $why, with no cut by the runner"
	fi
}

# dh_bg starts dockhand in the background, its output in a file of the
# row's, and sets DH_BG_PID; the file is DH_BG_LOG.
dh_bg() {
	local n
	n=$(find "$ROW_DIR" -maxdepth 1 -name 'bg.*.log' 2>/dev/null | wc -l | tr -d ' ')
	DH_BG_LOG="$ROW_DIR/bg.$((n + 1)).log"
	printf '$ dockhand %s &\n' "$*" >>"$ROW_DIR/out.log"
	"$DH_BIN" "$@" >"$DH_BG_LOG" 2>&1 &
	DH_BG_PID=$!
}

# dh_bg_wait waits for the background dockhand, adds its output to the
# row's log, and returns its exit status.
dh_bg_wait() {
	local status=0
	wait "$DH_BG_PID" || status=$?
	cat "$DH_BG_LOG" >>"$ROW_DIR/out.log"
	printf '[exit %d]\n' "$status" >>"$ROW_DIR/out.log"
	return "$status"
}

# wait_for_line waits until a file has a line matching a pattern, for up
# to some seconds; it fails where none comes.
wait_for_line() {
	local file=$1 pattern=$2 seconds=${3:-600} waited=0
	while [ "$waited" -lt "$seconds" ]; do
		grep -qE "$pattern" "$file" 2>/dev/null && return 0
		sleep 2
		waited=$((waited + 2))
	done
	return 1
}
