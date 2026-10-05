# stages: full
# A2: first-run setup in a fresh clone of the fork: checkout, worktrees,
# Tart image, and GitHub login, through setup. Each step says what it
# did, and a missing prerequisite is refused well, naming the fix.
act() {
	host_only "setup in dhtest's fresh fork clone" || return 0
	allow_change "*"
	(cd "$MACPORTS_TREE" && dh setup) || :
	# The login is auth login's: setup, without a terminal, says to run it
	# (the rc1 full stage, whose checkpoint asked for a code nothing had
	# printed). It runs here, printing its code and address, and waits
	# while the person enters the code.
	dh_bg auth login --no-browser
	if wait_for_line "$DH_BG_LOG" 'one-time code: ' 120; then
		checkpoint "authorize dockhand as the test account: $(grep -m1 'one-time code: ' "$DH_BG_LOG"), at $(grep -m1 -oE 'https://[^ ]+' "$DH_BG_LOG")" || { kill "$DH_BG_PID" 2>/dev/null; return 0; }
	fi
	dh_bg_wait || :
	dh auth status || :
	dh providers || :
}
assert() {
	grep -q '^\$ dockhand setup' "$ROW_DIR/out.log" || { row_fail "setup never ran"; return; }
	judged "setup said what each step did (checkout, worktrees, Tart image, login), and auth status names the test account"
}
