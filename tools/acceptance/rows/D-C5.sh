# stages: full
# D-C5: only the GitHub CLI's login: it's used, and auth status says it's
# the CLI's.
act() {
	host_only "the GitHub CLI's login" || return 0
	dh auth logout || :
	checkpoint "log the GitHub CLI in as the test account: gh auth login" || return 0
	dh_json auth status || :
	checkpoint "log dockhand back in: dockhand auth login" || return 0
}
assert() {
	jq -e '.. | strings | select(test("GitHub CLI|gh"))' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "auth status said it's the CLI's login" || row_fail "auth status didn't name the CLI"
}
