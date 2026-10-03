# stages: quick full
# D-S7: two serves at once: one leads and the other stands by.
act() {
	dh_bg serve
	DS7_FIRST=$DH_BG_PID DS7_FIRST_LOG=$DH_BG_LOG
	wait_for_line "$DH_BG_LOG" 'leading|serve:' 60 || :
	dh_bg serve
	DS7_SECOND=$DH_BG_PID DS7_SECOND_LOG=$DH_BG_LOG
	wait_for_line "$DH_BG_LOG" 'stand|leads|another|already' 60 || :
	sleep 5
	kill "$DS7_SECOND" "$DS7_FIRST" 2>/dev/null
	DH_BG_PID=$DS7_SECOND DH_BG_LOG=$DS7_SECOND_LOG dh_bg_wait || :
	DH_BG_PID=$DS7_FIRST DH_BG_LOG=$DS7_FIRST_LOG dh_bg_wait || :
}
assert() {
	if grep -qi 'leading' "$DS7_FIRST_LOG" && grep -qiE 'stand|leads|another serve|already' "$DS7_SECOND_LOG"; then
		row_pass "the first led; the second stood by: $(grep -iE 'stand|leads|another|already' "$DS7_SECOND_LOG" | head -1)"
	else
		row_fail "first: $(head -2 "$DS7_FIRST_LOG" | tr '\n' ';') second: $(head -2 "$DS7_SECOND_LOG" | tr '\n' ';')"
	fi
}
