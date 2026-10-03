# stages: full
# D-N4: GitHub's rate limit: outdated --all on a large set with a
# near-spent budget waits or says when the limit resets; a write is never
# retried.
act() {
	host_only "a near-spent GitHub budget" || return 0
	checkpoint "spend the test account's GitHub API budget to under 100 requests (gh api rate_limit)" || return 0
	dh_json outdated --all || :
}
assert() {
	jq -e '.. | strings | select(test("rate limit|resets"))' "$ROW_DIR/json/1.json" >/dev/null ||
		grep -qiE 'rate limit|resets' "$ROW_DIR/out.log" &&
		row_pass "waited or said when the limit resets" || row_fail "said nothing of the rate limit"
}
