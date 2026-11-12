# stages: quick full
# D-S6: two commands on one branch at once: a tidy while a check runs, and
# two checks. One waits or refuses, naming the other; neither corrupts the
# branch.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	# At the default level, as an agent reads it: off a terminal a
	# check says each step (34fa8f65), where rc5 needed -v (the docs
	# dig's I29).
	dh_bg check -p "$(port)"
	DS6_FIRST=$DH_BG_PID
	DS6_FIRST_LOG=$DH_BG_LOG
	# The overlap is shown only where the first check was building, and
	# still was, when each of the others started: a readiness wait that
	# timed out was ignored, and the two could run one after the other
	# (Codex's cold read at 2cdaf571, #232).
	if wait_for_line "$DH_BG_LOG" 'running here|building' 600 && kill -0 "$DS6_FIRST" 2>/dev/null; then
		DS6_OVERLAP=1
	fi
	dh_json check -p "$(port)" </dev/null || :
	kill -0 "$DS6_FIRST" 2>/dev/null || DS6_OVERLAP=""
	dh_json tidy -p "$(port)" -y </dev/null || :
	DH_BG_PID=$DS6_FIRST DH_BG_LOG=$DS6_FIRST_LOG dh_bg_wait || :
}
assert() {
	local second tidy
	[ -n "${DS6_OVERLAP:-}" ] || { row_result "not exercised" "the first check wasn't building throughout the second check, so the two never overlapped"; return; }
	second=$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)
	tidy=$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)
	if [ "$second" != 0 ] && ! jq -r '.error // ""' "$ROW_DIR/json/1.json" | matches -iE 'check-[0-9]+|already|running|wait'; then
		row_fail "the second check exited $second without naming the first: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"
	elif [ "$tidy" != 0 ] && ! jq -r '.error // ""' "$ROW_DIR/json/2.json" | matches -iE 'check|running|wait|another'; then
		row_fail "tidy exited $tidy without naming what holds the branch: $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"
	else
		row_pass "the second check $( [ "$second" = 0 ] && echo waited || echo refused, naming the first); tidy $( [ "$tidy" = 0 ] && echo waited and applied || echo refused, naming the check)"
	fi
}
