# stages: full
# A2: first-run setup in a fresh clone of the fork: checkout, worktrees,
# Tart image, and GitHub login, through setup. Each step says what it
# did, and a missing prerequisite is refused well, naming the fix.
act() {
	host_only "setup in dhtest's fresh fork clone" || return 0
	allow_change "*"
	(cd "$MACPORTS_TREE" && dh setup) || :
	# The login is setup github's: setup, without a terminal, says to run it
	# (the rc1 full stage, whose checkpoint asked for a code nothing had
	# printed). It runs here, printing its code and address, and waits
	# while the person enters the code.
	# The code is issued once the person says they're there (device_login).
	device_login "A2 logs dockhand in as the test account" || :
	dh auth status || :
	dh providers || :
}
assert() {
	grep -q '^\$ dockhand setup' "$ROW_DIR/out.log" || { row_fail "setup never ran"; return; }
	judged "setup said what each step did (checkout, worktrees, Tart image, login), and auth status names the test account"
}
