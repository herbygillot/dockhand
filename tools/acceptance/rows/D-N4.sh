# stages: full
# D-N4: GitHub's rate limit: outdated over named ports with a near-spent
# budget says when the limit resets, with its time zone; leads with the
# ports it couldn't check, never saying none of them has a newer release;
# and exits 3. A write is never retried.
#
# It names the run's two ports and two more, where outdated --all, which
# names none, was refused (the rc6 full stage); and it reads what's left
# of the budget from a real response's X-RateLimit-Remaining, since gh api
# rate_limit said 5000 where the headers said 0.
act() {
	host_only "a near-spent GitHub budget" || return 0
	checkpoint "spend the test account's GitHub API budget to under 100 requests" || return 0
	gh api -i repos/macports/macports-ports 2>/dev/null | grep -i '^x-ratelimit-remaining:' >"$ROW_DIR/remaining" || :
	dh_json outdated "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}" ${ACCEPT_DN4_PORTS:-libt3config contacts-cli} || :
}
assert() {
	local file=$ROW_DIR/json/1.json
	grep -qiE 'rate limit .*resets in [0-9]+ [a-z]+, at [0-9]{2}:[0-9]{2} [A-Z]+' "$file" "$ROW_DIR/out.log" ||
		{ row_fail "said nothing of when the limit resets, with its zone (budget: $(cat "$ROW_DIR/remaining" 2>/dev/null))"; return; }
	if [ "$(jq -r '.result.unchecked // 0' "$file")" != 0 ]; then
		[ "$(cat "$file.exit")" = 3 ] || { row_fail "some couldn't be checked, and it exited $(cat "$file.exit"), not 3"; return; }
		grep -q "None of .* has a newer release" "$ROW_DIR/out.log" && { row_fail "it said none had a newer release, with some unchecked"; return; }
	fi
	row_pass "said when the limit resets, with its zone, and led with what it couldn't check"
}
