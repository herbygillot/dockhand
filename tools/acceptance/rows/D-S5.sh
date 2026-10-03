# stages: quick full
# D-S5: a branch renamed with git branch -m is found again, by its
# worktree.
act() {
	dh_setup start ds5-before --port "${ACCEPT_GO_PORT:?}" || return 0
	allow_ref_gone refs/heads/dockhand/ds5-before
	git -C "$MACPORTS_TREE" branch -m dockhand/ds5-before dockhand/ds5-after
	DS5_DIR=$("$DH_BIN" path ds5-before 2>/dev/null || :)
	dh_json status || :
}
assert() {
	if jq -e '.result.branches[]? | select(.git_branch == "dockhand/ds5-after")' "$ROW_DIR/json/1.json" >/dev/null; then
		row_pass "status follows the renamed branch"
	else
		row_fail "status lost the renamed branch: $(jq -c '[.result.branches[]? | {name, git_branch, missing}]' "$ROW_DIR/json/1.json")"
	fi
}
