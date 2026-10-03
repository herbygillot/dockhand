# stages: full
# D-S10: another contributor's PR lands the same update first: status
# says the branch landed by another route and offers archive; nothing is
# pushed over it.
act() {
	host_only "an update landed by someone else" || return 0
	checkpoint "name a branch of this run whose update has landed on master by another PR in $ROW_DIR/branch" || return 0
	dh_json status "$(cat "$ROW_DIR/branch")" || :
}
assert() {
	jq -e '.result.branches[0].on_master // empty' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "status said it landed by another route" || row_fail "status didn't say it landed elsewhere"
}
