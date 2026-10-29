# stages: full
# G14: two local checks on one prefix at once (v0.6.0's rc7). One is run in
# the background, -d, and a second, on another branch, in the foreground
# while the first builds: the second says it's waiting for the prefix,
# held by the first check, and builds once the first has put the prefix
# back; both pass, and the prefix's ports are as they were. It builds in
# ACCEPT_LOCAL_PREFIX, G3's prefix installed without root, so no sudo
# asks; without one, the row isn't run.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a MacPorts prefix installed without root" || return 0
	G14_PREFIX=${ACCEPT_LOCAL_PREFIX:-}
	if [ -z "$G14_PREFIX" ] || [ ! -x "$G14_PREFIX/bin/port" ]; then
		row_result "not run" "set ACCEPT_LOCAL_PREFIX to a MacPorts prefix dhtest installed without root"
		G14_PREFIX=""
		return 0
	fi
	local_active "$G14_PREFIX" >"$ROW_DIR/active.before"
	G14_FIRST=$(run_name g14-first)
	G14_SECOND=$(run_name g14-second)
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G14_FIRST" --subject "rebuild to check twice in one prefix" || return 0
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G14_SECOND" --subject "rebuild to check twice in one prefix" || return 0
	dh_json check -b "$G14_FIRST" --on "local:$G14_PREFIX" -d </dev/null || :
	G14_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json" 2>/dev/null)
	[ -n "$G14_RUN" ] || return 0
	# The second starts once the first is building there.
	local i
	for i in $(seq 1 60); do
		"$DH_BIN" --json status -b "$G14_FIRST" 2>/dev/null | matches -i running && break
		sleep 5
	done
	dh check -b "$G14_SECOND" --on "local:$G14_PREFIX" -v </dev/null || :
	dh_json wait "$G14_RUN" </dev/null || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G14_PREFIX:-}" ] || return 0
	[ -n "${G14_RUN:-}" ] || { row_fail "check -d queued no check: $(jq -r '.error // empty' "$ROW_DIR/json/1.json" 2>/dev/null)"; return; }
	local second
	second=$(sed -n "/^\\\$ dockhand check -b $G14_SECOND/,/^\\[exit/p" "$ROW_DIR/out.log")
	printf '%s\n' "$second" | matches -F "[exit 0]" || { row_fail "the second check failed: $(printf '%s\n' "$second" | tail -3 | tr '\n' ' ')"; return; }
	printf '%s\n' "$second" | matches -F "waiting for $G14_PREFIX, held by $G14_RUN" ||
		{ row_fail "the second check didn't say it waited for $G14_PREFIX, held by $G14_RUN"; return; }
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 0 ] || { row_fail "$G14_RUN didn't pass: $(jq -r '.error // empty' "$ROW_DIR/json/2.json" 2>/dev/null)"; return; }
	local_same "$G14_PREFIX" "$ROW_DIR/active.before" "the two checks left $G14_PREFIX's active ports changed" || return
	row_pass "the second check waited for $G14_PREFIX, held by $G14_RUN, then built; both passed, and the prefix's ports are as they were"
}
