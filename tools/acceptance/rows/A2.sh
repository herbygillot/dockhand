# stages: full
# A2: first-run setup in a fresh clone of the fork: checkout, worktrees,
# Tart image, and GitHub login, through setup. Each step says what it
# did, and a missing prerequisite is refused well, naming the fix.
act() {
	host_only "setup in dhtest's fresh fork clone" || return 0
	allow_change "*"
	(cd "$MACPORTS_TREE" && dh setup) || :
	checkpoint "enter auth login's code in a browser, signed in as the test account" || return 0
	dh auth status || :
	dh providers || :
}
assert() {
	grep -q '^\$ dockhand setup' "$ROW_DIR/out.log" || { row_fail "setup never ran"; return; }
	judged "setup said what each step did (checkout, worktrees, Tart image, login), and auth status names the test account"
}
