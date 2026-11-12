# stages: full
# G33: a pull request's merge fitness in status (fc5f2e4e, 85fd5fb9).
# Once a test pull request of the small Go port's update is open in the
# sandbox, opened by the test account, which maintains no port, status
# --json gives pull_request.merge, with the port and its maintainers, read
# from master's port index where no change record keeps them, and the
# 72-hour maintainer timeout's end; status's row says "maintainer timeout
# ends in …" (or "passed", or the port is nomaintainer, which waits on
# nothing), and its detail adds Merge lines. It needs a pull request on
# GitHub, so the full stage alone.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a pull request on GitHub" || return 0
	dh_setup update "$(port)" --new || return 0
	G33_BRANCH=$(dh_quiet --json status --port "$(port)" | jq -r '.result.branches[0].name')
	dh check -b "$G33_BRANCH" || return 0
	dh tidy -b "$G33_BRANCH" -y || return 0
	submit_pr "$(port)" "$G33_BRANCH" || return 0
	allow_change "*"
	dh status </dev/null || :
	dh status "$G33_BRANCH" </dev/null || :
	dh_json status "$G33_BRANCH" </dev/null || :
	# submit_pr's envelope comes first, so status's is named, not numbered
	# (rc4's full stage read submit's as status's).
	G33_STATUS=$DH_LAST_JSON
	close_test_pr "$(port)" "$G33_BRANCH"
}
assert() {
	local json=${G33_STATUS:-} merge
	[ -n "$json" ] && [ -f "$json" ] || { row_fail "status didn't run after the pull request opened"; return; }
	merge=$(jq -c '[.. | objects | .merge? | objects][0] // empty' "$json")
	[ -n "$merge" ] || { row_fail "status --json gave no pull_request.merge for the open pull request"; return; }
	if [ "$(printf '%s' "$merge" | jq --arg port "$(port)" '[.ports[]? | select(.port == $port)] | length')" = 0 ]; then
		row_fail "merge fitness names no $(port): $merge"
		return
	fi
	if printf '%s' "$merge" | jq -e --arg port "$(port)" '.ports[] | select(.port == $port) | .nomaintainer != true and (.maintainers | length) == 0' >/dev/null 2>&1 &&
		printf '%s' "$merge" | jq -e '(.maintainers_unread // []) | length > 0' >/dev/null; then
		row_fail "$(port)'s maintainers weren't read, from master's index or a change record: $merge"
		return
	fi
	if ! grep -E 'maintainer timeout (ends in|passed)|nomaintainer' "$ROW_DIR/out.log" | matches .; then
		row_fail "status's row didn't say the maintainer timeout"
		return
	fi
	if ! grep -E '^ *Merge ' "$ROW_DIR/out.log" | matches .; then
		row_fail "status's detail gave no Merge lines"
		return
	fi
	row_pass "merge fitness: $(grep -E '^ *Merge ' "$ROW_DIR/out.log" | head -2 | sed 's/^ *//' | tr '\n' ' ')"
}
