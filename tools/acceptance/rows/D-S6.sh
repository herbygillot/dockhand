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
	# --fresh for both, so each builds: rc4's two checks reused
	# check-1023, and nothing overlapped.
	dh_bg check -p "$(port)" --fresh
	DS6_FIRST=$DH_BG_PID
	DS6_FIRST_LOG=$DH_BG_LOG
	# The others start only while the first is building: a readiness
	# wait that timed out was ignored, and the two could run one after
	# the other (Codex's cold read at 2cdaf571, #232).
	wait_for_line "$DH_BG_LOG" 'building in' 600 && kill -0 "$DS6_FIRST" 2>/dev/null || return 0
	DS6_RUN=$(grep -oE 'check-[0-9]+' "$DS6_FIRST_LOG" | head -1)
	DS6_OVERLAP=1
	# The second check and the tidy both start while the first builds, in
	# the background: a second check joins or waits for the first and ends
	# with it, so the first never outlives it (rc4's D-S6, check-1092),
	# and a tidy run after the second returned met no build at all.
	dh_bg check -p "$(port)" --fresh
	DS6_SECOND=$DH_BG_PID DS6_SECOND_LOG=$DH_BG_LOG
	dh_bg tidy -p "$(port)" -y
	DS6_TIDY=$DH_BG_PID DS6_TIDY_LOG=$DH_BG_LOG
	kill -0 "$DS6_FIRST" 2>/dev/null && DS6_TIDY_DURING=1
	DS6_SECOND_EXIT=0
	DH_BG_PID=$DS6_SECOND DH_BG_LOG=$DS6_SECOND_LOG dh_bg_wait || DS6_SECOND_EXIT=$?
	DS6_TIDY_EXIT=0
	DH_BG_PID=$DS6_TIDY DH_BG_LOG=$DS6_TIDY_LOG dh_bg_wait || DS6_TIDY_EXIT=$?
	DH_BG_PID=$DS6_FIRST DH_BG_LOG=$DS6_FIRST_LOG dh_bg_wait || :
}
assert() {
	[ -n "${DS6_OVERLAP:-}" ] || { row_result "not exercised" "the first check never said it was building, so nothing could start beside it"; return; }
	# The second joined or waited for the first, naming its check, or
	# refused naming it.
	grep -qF "${DS6_RUN:-check-}" "$DS6_SECOND_LOG" ||
		{ row_fail "the second check (exit $DS6_SECOND_EXIT) didn't name ${DS6_RUN:-the first check}: $(tail -3 "$DS6_SECOND_LOG" | tr '\n' ' ')"; return; }
	[ -n "${DS6_TIDY_DURING:-}" ] || { row_result "not exercised" "the second check passed beside ${DS6_RUN}, but the first had ended before the tidy started, so the tidy-during-a-build half wasn't seen"; return; }
	if [ "$DS6_TIDY_EXIT" != 0 ] && ! said -i 'check|running|wait|another' "$DS6_TIDY_LOG"; then
		row_fail "tidy, started while ${DS6_RUN} built, exited $DS6_TIDY_EXIT without naming what holds the branch: $(tail -3 "$DS6_TIDY_LOG" | tr '\n' ' ')"
		return
	fi
	row_pass "the second check named ${DS6_RUN} and $( [ "$DS6_SECOND_EXIT" = 0 ] && echo joined or waited for it || echo refused); tidy, started while it built, $( [ "$DS6_TIDY_EXIT" = 0 ] && echo waited and applied || echo refused, naming the check)"
}
