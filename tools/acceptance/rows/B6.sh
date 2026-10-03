# stages: full
# B6: after a merge, clean removes the worktree, the local branch, and
# the fork branch, and the record stays in status --all.
act() {
	host_only "a merged pull request" || return 0
	checkpoint "merge one of this run's real updates (or name one already merged in $ROW_DIR/branch)" || return 0
	B6_BRANCH=$(cat "$ROW_DIR/branch" 2>/dev/null)
	allow_change "*"
	allow_ref_gone "*"
	dh status --refresh || :
	dh_json clean -y || :
	dh_json status --all || :
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ] || { row_fail "clean failed"; return; }
	if jq -e --arg b "$B6_BRANCH" '.result.branches[] | select(.name == $b)' "$ROW_DIR/json/2.json" >/dev/null; then
		row_pass "cleaned, and the record stays in status --all"
	else
		row_fail "the merged branch's record is gone from status --all"
	fi
}
