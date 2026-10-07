# stages: full
# D-S2: the PR merged, or closed, on GitHub while the branch has local
# edits: status --refresh says so, clean keeps the edits, and rebase
# doesn't point at the merged PR.
#
# The row makes its test pull request, closes it, and leaves an
# uncommitted edit in setup, before the harm sweep's snapshot: a person
# made them inside it, where H2 and H3 took them for the row's harm (the
# rc6 full stage).
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "a pull request on GitHub" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull request for $(port) isn't an approved test one, and it closes what it opens"
		return 0
	fi
	dh_setup update "$(port)" --new || return 1
	DS2_BRANCH=$(own_branch)
	dh tidy -b "$DS2_BRANCH" -y || return 1
	submit_pr "$(port)" "$DS2_BRANCH" --no-check || return 1
	next_superseded_for "$DS2_BRANCH" "its test pull request closed"
	gh pr close "$(dh_quiet --json status "$DS2_BRANCH" | jq -r '.result.branches[0].pull_request.url // empty')" >>"$ROW_DIR/out.log" 2>&1 || return 1
	printf '# an edit no commit has\n' >>"$(dh_quiet path "$DS2_BRANCH")/$(dh_quiet --json status "$DS2_BRANCH" | jq -r '.result.branches[0].directories[0]')/Portfile"
}
act() {
	host_only "a pull request on GitHub" || return 0
	[ -n "${DS2_BRANCH:-}" ] || return 0
	allow_change "*"
	dh_json status --refresh "$DS2_BRANCH" || :
	dh_json clean --closed -y || :
	dh rebase -b "$DS2_BRANCH" || :
}
assert() {
	jq -e '.result.branches[0].pull_request.state | select(. == "closed" or . == "merged")' "$ROW_DIR/json/1.json" >/dev/null ||
		{ row_fail "status --refresh didn't say the PR closed"; return; }
	judged "clean kept the edits, and rebase didn't point at the closed PR"
}
