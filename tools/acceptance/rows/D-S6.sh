# stages: quick full
# D-S6: two commands on one branch at once: a tidy while a check runs, and
# two checks. One waits or refuses, naming the other; neither corrupts the
# branch.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	dh_bg check -p "$(port)"
	wait_for_line "$DH_BG_LOG" 'runs here|building' 600 || :
	DS6_FIRST=$DH_BG_PID
	DS6_FIRST_LOG=$DH_BG_LOG
	dh_json check -p "$(port)" </dev/null || :
	dh_json tidy -p "$(port)" -y </dev/null || :
	DH_BG_PID=$DS6_FIRST DH_BG_LOG=$DS6_FIRST_LOG dh_bg_wait || :
}
assert() {
	local second tidy
	second=$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)
	tidy=$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)
	if [ "$second" != 0 ] && ! jq -r '.error // ""' "$ROW_DIR/json/1.json" | grep -qiE 'check-[0-9]+|already|running|wait'; then
		row_fail "the second check exited $second without naming the first: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"
	elif [ "$tidy" != 0 ] && ! jq -r '.error // ""' "$ROW_DIR/json/2.json" | grep -qiE 'check|running|wait|another'; then
		row_fail "tidy exited $tidy without naming what holds the branch: $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"
	else
		row_pass "the second check $( [ "$second" = 0 ] && echo waited || echo refused, naming the first); tidy $( [ "$tidy" = 0 ] && echo waited and applied || echo refused, naming the check)"
	fi
}
