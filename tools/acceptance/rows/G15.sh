# stages: full
# G15: a local check whose dockhand is killed mid-build, its guest program
# left building, and another check on the same prefix (272e968b): the
# second waits for the first's program to end, saying "waiting for
# check-N's program (pid P), which its dockhand left running, to finish in
# <prefix>", puts back what the first left changed, then builds; it
# passes, and the prefix's ports are as they were. It builds in
# ACCEPT_LOCAL_PREFIX, installed without root, so no sudo asks, with
# ACCEPT_SLEEP_PORT's build long enough to be caught in; without them, the
# row isn't run.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a MacPorts prefix installed without root" || return 0
	G15_PREFIX=${ACCEPT_LOCAL_PREFIX:-}
	if [ -z "$G15_PREFIX" ] || [ ! -x "$G15_PREFIX/bin/port" ] || [ -z "${ACCEPT_SLEEP_PORT:-}" ]; then
		row_result "not run" "set ACCEPT_LOCAL_PREFIX to a MacPorts prefix dhtest installed without root, and ACCEPT_SLEEP_PORT to a port whose build lasts a few minutes"
		G15_PREFIX=""
		return 0
	fi
	local_active "$G15_PREFIX" >"$ROW_DIR/active.before"
	G15_FIRST=$(run_name g15-killed)
	G15_SECOND=$(run_name g15-after)
	dh_setup revbump "$ACCEPT_SLEEP_PORT" --new="$G15_FIRST" --subject "rebuild to stop mid-build" || return 0
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G15_SECOND" --subject "rebuild to check after a killed check" || return 0
	dh_bg check -b "$G15_FIRST" --on "local:$G15_PREFIX"
	if ! wait_for_line "$DH_BG_LOG" "building .*: (fetch|checksum|install)" 1800; then
		kill "$DH_BG_PID" 2>/dev/null
		dh_bg_wait || :
		row_fail "the first check never reached its build in $G15_PREFIX"
		return 0
	fi
	# dockhand alone is killed; its guest program goes on building.
	kill -9 "$DH_BG_PID" 2>/dev/null
	dh_bg_wait || :
	dh check -b "$G15_SECOND" --on "local:$G15_PREFIX" -v </dev/null || :
	dh cancel -b "$G15_FIRST" || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G15_PREFIX:-}" ] || return 0
	local second
	second=$(sed -n "/^\\\$ dockhand check -b $G15_SECOND/,/^\\[exit/p" "$ROW_DIR/out.log")
	printf '%s\n' "$second" | matches -E "waiting for check-[0-9]+'s program \\(pid [0-9]+\\), which its dockhand left running, to finish in $G15_PREFIX" ||
		{ row_fail "the second check didn't say it waited for the killed check's program in $G15_PREFIX: $(printf '%s\n' "$second" | grep -i wait | head -2 | tr '\n' ' ')"; return; }
	printf '%s\n' "$second" | matches -F "[exit 0]" || { row_fail "the second check failed: $(printf '%s\n' "$second" | tail -3 | tr '\n' ' ')"; return; }
	local_same "$G15_PREFIX" "$ROW_DIR/active.before" "the checks left $G15_PREFIX's active ports changed" || return
	row_pass "the second check waited for the killed check's program to finish in $G15_PREFIX, put back what it left, built, and passed; the prefix's ports are as they were"
}
