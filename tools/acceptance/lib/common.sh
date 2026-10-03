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

# dh_json runs dockhand with --json once, keeping the envelope and the
# process's exit status for H8, and returns that status. Its output is
# also in out.log, for H4.
dh_json() {
	local status=0 n
	n=$(find "$ROW_DIR/json" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
	local file="$ROW_DIR/json/$((n + 1)).json"
	printf '$ dockhand --json %s\n' "$*" >>"$ROW_DIR/out.log"
	"$DH_BIN" --json "$@" >"$file" 2>>"$ROW_DIR/out.log" || status=$?
	cat "$file" >>"$ROW_DIR/out.log"
	printf '%s\n' "$status" >"$file.exit"
	printf '%s\n' "$*" >"$file.args"
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
