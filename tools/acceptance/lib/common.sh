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
# that would hang reads as exit 142 (SIGALRM) rather than holding the run.
with_timeout() {
	local seconds=$1
	shift
	perl -e 'alarm shift; exec @ARGV' "$seconds" "$@"
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
