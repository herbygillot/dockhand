# stages: full
# B4: answering a review: an edit in the worktree, check, tidy, and
# submit again. The PR is updated, not replaced; a person's edits to the
# description are kept; a review re-request is offered.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a pull request on GitHub" || return 0
	dh update "$(port)" --new || return 0
	B4_BRANCH=$(dh_quiet --json status --port "$(port)" | jq -r '.result.branches[0].name')
	dh check -b "$B4_BRANCH" || return 0
	dh tidy -b "$B4_BRANCH" -y || return 0
	submit_pr "$(port)" "$B4_BRANCH" || return 0
	checkpoint "edit the description of the test PR for $(port) on GitHub, and leave a review asking for a change" || return 0
	printf '\n# answering a review\n' >>"$(dh_quiet path "$B4_BRANCH")/$(dh_quiet --json status "$B4_BRANCH" | jq -r '.result.branches[0].directories[0]')/Portfile"
	allow_change "*"
	dh check -b "$B4_BRANCH" || :
	dh tidy -b "$B4_BRANCH" -y || :
	dh_json submit -b "$B4_BRANCH" -y || :
	close_test_pr "$(port)" "$B4_BRANCH"
}
assert() {
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 0 ] || { row_fail "the second submit failed"; return; }
	judged "the second submit updated the same PR, kept the description's edit, and offered the review re-request"
}
