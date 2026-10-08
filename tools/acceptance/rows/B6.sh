# stages: full
# B6: after a merge, clean removes the worktree, the local branch, and
# the fork branch, and the record stays in status --all.
#
# The row makes its own merged pull request in setup, before the harm
# sweep's snapshot: a test one in the sandbox, merged there with gh, and
# the sandbox's master set back to MacPorts' after. A checkpoint had a
# person make one inside the snapshot's window, where H2 and H3 took the
# pull request and the merge for the row's harm (the rc6 full stage).
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "a merged pull request" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull request for $(port) isn't an approved test one, and it merges what it opens"
		return 0
	fi
	dh_setup update "$(port)" --new || return 1
	B6_BRANCH=$(own_branch)
	dh check -b "$B6_BRANCH" || return 1
	dh tidy -b "$B6_BRANCH" -y || return 1
	submit_pr "$(port)" "$B6_BRANCH" || return 1
	local url
	url=$(dh_quiet --json status "$B6_BRANCH" | jq -r '.result.branches[0].pull_request.url // empty')
	[ -n "$url" ] || return 1
	next_superseded_for "$B6_BRANCH" "its test pull request merged"
	gh pr merge "$url" --merge >>"$ROW_DIR/out.log" 2>&1 || return 1
	b6_reset_sandbox
	printf '%s\n' "$B6_BRANCH" >"$ROW_DIR/branch"
}

# b6_reset_sandbox sets the sandbox's master back to MacPorts' once the
# merge is in it, so later test pull requests show only their own commits.
b6_reset_sandbox() {
	local upstream
	upstream=$(gh api repos/macports/macports-ports/git/ref/heads/master --jq .object.sha 2>>"$ROW_DIR/out.log") || return 0
	gh api -X PATCH "repos/${DOCKHAND_PULL_REQUESTS:?}/git/refs/heads/master" -f sha="$upstream" -F force=true --silent >>"$ROW_DIR/out.log" 2>&1 || :
}

act() {
	host_only "a merged pull request" || return 0
	B6_BRANCH=$(cat "$ROW_DIR/branch" 2>/dev/null)
	[ -n "$B6_BRANCH" ] || return 0
	allow_change "*"
	allow_ref_gone "*"
	dh status --refresh || :
	dh_json clean -y || :
	dh_json status --all || :
}
assert() {
	local clean status
	clean=$(grep -l '^clean -y$' "$ROW_DIR"/json/*.json.args 2>/dev/null | head -1)
	status=$(grep -l '^status --all$' "$ROW_DIR"/json/*.json.args 2>/dev/null | head -1)
	[ "$(cat "${clean%.args}.exit" 2>/dev/null)" = 0 ] || { row_fail "clean failed"; return; }
	if jq -e --arg b "$B6_BRANCH" '.result.branches[] | select(.name == $b)' "${status%.args}" >/dev/null; then
		row_pass "cleaned, and the record stays in status --all"
	else
		row_fail "the merged branch's record is gone from status --all"
	fi
}
