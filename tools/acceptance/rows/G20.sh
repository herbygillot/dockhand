# stages: full
# G20: submit --draft on a ready pull request returns it to draft before
# it pushes (Codex's workflow review of 2ad896d2, W3). The row opens a test
# pull request for the small Go port, marks it ready, makes another commit
# on its branch, and submits that with --draft: GitHub then has the pull
# request as a draft, at the branch's new head. It closes the pull request
# once it has its evidence.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

setup() {
	host_only "a ready test pull request" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull request for $(port) isn't an approved test one, and it closes what it opens"
		return 0
	fi
	dh_setup update "$(port)" --new || return 1
	G20_BRANCH=$(own_branch)
	dh tidy -b "$G20_BRANCH" -y || return 1
	submit_pr "$(port)" "$G20_BRANCH" --no-check || return 1
	G20_URL=$(dh_quiet --json status "$G20_BRANCH" | jq -r '.result.branches[0].pull_request.url // empty')
	[ -n "$G20_URL" ] || { row_fail "submit opened no pull request for $(port)"; return 1; }
	gh pr ready "$G20_URL" >>"$ROW_DIR/out.log" 2>&1 || :
	[ "$(gh pr view "$G20_URL" --json isDraft --jq .isDraft 2>/dev/null)" = false ] ||
		{ row_result "not run" "the test pull request couldn't be made ready to begin with"; return 0; }
}

act() {
	host_only "a ready test pull request" || return 0
	[ -n "${G20_URL:-}" ] && [ ! -f "$ROW_DIR/result" ] || return 0
	dh revbump "$(port)" -b "$G20_BRANCH" --subject "rebuild to submit as a draft" || :
	dh tidy -b "$G20_BRANCH" -y || :
	# A draft goes up whatever its check found, with none too: --draft with
	# --no-check is refused (rc1's full stage).
	submit_pr "$(port)" "$G20_BRANCH" --draft || :
	gh pr view "$G20_URL" --json isDraft,headRefOid >"$ROW_DIR/after.json" 2>>"$ROW_DIR/out.log" || :
	git -C "$("$DH_BIN" path "$G20_BRANCH")" rev-parse HEAD >"$ROW_DIR/head" 2>/dev/null || :
	close_test_pr "$(port)" "$G20_BRANCH"
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/after.json" ] || return 0
	[ "$(jq -r .isDraft "$ROW_DIR/after.json")" = true ] || { row_fail "after submit --draft, $G20_URL isn't a draft"; return; }
	[ "$(jq -r .headRefOid "$ROW_DIR/after.json")" = "$(cat "$ROW_DIR/head")" ] || { row_fail "$G20_URL isn't at the branch's new head after submit --draft"; return; }
	row_pass "submit --draft returned the ready $G20_URL to draft and pushed the branch's new head"
}
