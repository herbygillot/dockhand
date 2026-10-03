# stages: full
# D-S2: the PR merged, or closed, on GitHub while the branch has local
# edits: status --refresh says so, clean keeps the edits, and rebase
# doesn't point at the merged PR.
act() {
	host_only "a pull request on GitHub" || return 0
	checkpoint "close a test PR of this run on GitHub, after an uncommitted edit in its worktree; name the branch in $ROW_DIR/branch" || return 0
	local branch
	branch=$(cat "$ROW_DIR/branch")
	dh_json status --refresh "$branch" || :
	dh_json clean --closed -y || :
	dh rebase -b "$branch" || :
}
assert() {
	jq -e '.result.branches[0].pull_request.state | select(. == "closed" or . == "merged")' "$ROW_DIR/json/1.json" >/dev/null ||
		{ row_fail "status --refresh didn't say the PR closed"; return; }
	judged "clean kept the edits, and rebase didn't point at the closed PR"
}
