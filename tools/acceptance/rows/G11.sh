# stages: full
# G11: start says another open pull request (v0.7.0). With a test pull
# request open for the small Go port in the sandbox, titled "[testing]
# <port>: …" as the stage titles them, start --port <port> names it before
# the work, "Also open for <port>: #N …", from one search of the port, and
# stops for nothing. The row opens the pull request in its setup, waits for
# GitHub's search to find it, and closes it once start has spoken.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "an open test pull request" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull request for $(port) isn't an approved test one, and it closes what it opens"
		return 0
	fi
	dh_setup update "$(port)" --new || return 1
	G11_BRANCH=$(own_branch)
	dh tidy -b "$G11_BRANCH" -y || return 1
	submit_pr "$(port)" "$G11_BRANCH" --no-check || return 1
	G11_NUMBER=$(dh_quiet --json status "$G11_BRANCH" | jq -r '.result.branches[0].pull_request.number // empty')
	[ -n "$G11_NUMBER" ] || { row_fail "submit opened no pull request for $(port)"; return 1; }
	# GitHub's search indexes a new pull request in a while; start asks it.
	local i
	for i in $(seq 1 24); do
		gh api -X GET search/issues -f q="repo:${DOCKHAND_PULL_REQUESTS:?} is:pr is:open in:title $(port)" --jq '.items[].number' 2>>"$ROW_DIR/out.log" |
			matches -x "$G11_NUMBER" && return 0
		sleep 5
	done
	row_result "not run" "GitHub's search didn't find #$G11_NUMBER within two minutes, so start couldn't have"
}
act() {
	host_only "an open test pull request" || return 0
	[ -n "${G11_NUMBER:-}" ] && [ ! -f "$ROW_DIR/result" ] || return 0
	dh start --port "$(port)" || :
	sed -n '/^\$ dockhand start/,/^\[exit/p' "$ROW_DIR/out.log" >"$ROW_DIR/start.log"
	close_test_pr "$(port)" "$G11_BRANCH"
}
assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/start.log" ] || return 0
	matches -F '[exit 0]' "$ROW_DIR/start.log" || { row_fail "start --port $(port) failed: $(tail -3 "$ROW_DIR/start.log" | tr '\n' ';')"; return; }
	if ! matches -E "^Also open for $(port): #$G11_NUMBER " "$ROW_DIR/start.log"; then
		row_fail "start didn't name #$G11_NUMBER, open for $(port): $(tr '\n' ';' <"$ROW_DIR/start.log")"
		return
	fi
	# Said before the next step, the work.
	sed -n '/^Also open for/,$p' "$ROW_DIR/start.log" | matches '^Next: ' ||
		{ row_fail "start named #$G11_NUMBER after its Next: line"; return; }
	row_pass "start --port $(port) named #$G11_NUMBER, the test pull request open for it, before its Next: line, and stopped for nothing"
}
