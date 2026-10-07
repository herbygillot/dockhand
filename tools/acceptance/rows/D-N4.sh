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
#
# The row spends what's left itself, to 0, where at 87 left outdated's four
# ports fit in it and it simply succeeded (the rc8 full stage); and it
# waits for the limit to reset before it ends, up to 65 minutes, since a
# spent budget leaves every later GitHub row rate-limited until it does.

# dn4_header is a header of a real response, as read for the budget.
dn4_header() { gh api -i repos/macports/macports-ports 2>/dev/null | tr -d '\r' | awk -v h="$1" 'tolower($1) == tolower(h) ":" {print $2; exit}'; }

act() {
	host_only "a near-spent GitHub budget" || return 0
	checkpoint "spend the test account's GitHub API budget to under 100 requests" || return 0
	local left n=0
	left=$(dn4_header x-ratelimit-remaining)
	printf 'x-ratelimit-remaining: %s\n' "$left" >"$ROW_DIR/remaining"
	while [ -n "$left" ] && [ "$left" -gt 0 ] && [ "$n" -lt 150 ]; do
		left=$(dn4_header x-ratelimit-remaining)
		n=$((n + 1))
	done
	printf 'spent to %s in %d requests\n' "${left:-unknown}" "$n" >>"$ROW_DIR/remaining"
	dh_json outdated "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}" ${ACCEPT_DN4_PORTS:-libt3config contacts-cli} || :
	dh_json update "${ACCEPT_GO_PORT:?}" --new --plan || :
	dn4_wait_reset
}

# dn4_wait_reset waits for the budget to come back, so the rows after
# aren't rate-limited by this one, up to 65 minutes.
dn4_wait_reset() {
	local reset now
	# A real response's header, as for the budget; rate_limit's where none.
	reset=$(dn4_header x-ratelimit-reset)
	[ -n "$reset" ] || reset=$(gh api rate_limit --jq .resources.core.reset 2>/dev/null)
	now=$(date +%s)
	if [ -n "$reset" ] && [ "$reset" -gt "$now" ] && [ $((reset - now)) -le 3900 ]; then
		printf 'waiting %d seconds for the limit to reset\n' "$((reset - now + 30))" >>"$ROW_DIR/remaining"
		sleep "$((reset - now + 30))"
	fi
}
assert() {
	local file=$ROW_DIR/json/1.json
	grep -qiE 'rate limit .*resets in [0-9]+ [a-z]+, at [0-9]{2}:[0-9]{2} [A-Z]+' "$file" "$ROW_DIR/out.log" ||
		{ row_fail "said nothing of when the limit resets, with its zone (budget: $(cat "$ROW_DIR/remaining" 2>/dev/null))"; return; }
	[ "$(jq -r '.result.unchecked // 0' "$file")" != 0 ] ||
		{ row_fail "every port was checked, so the limit was never met ($(tr '\n' ' ' <"$ROW_DIR/remaining"))"; return; }
	if [ "$(jq -r '.result.unchecked // 0' "$file")" != 0 ]; then
		[ "$(cat "$file.exit")" = 3 ] || { row_fail "some couldn't be checked, and it exited $(cat "$file.exit"), not 3"; return; }
		grep -q "None of .* has a newer release" "$ROW_DIR/out.log" && { row_fail "it said none had a newer release, with some unchecked"; return; }
	fi
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 1 ] && jq -r '.error // ""' "$ROW_DIR/json/2.json" | grep -q 'nothing was changed' ||
		{ row_fail "update --plan, rate-limited, didn't refuse saying nothing was changed: exit $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null), $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"; return; }
	row_pass "said when the limit resets, with its zone, and led with what it couldn't check; update said nothing was changed"
}
