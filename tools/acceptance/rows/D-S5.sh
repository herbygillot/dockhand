# stages: quick full
# D-S5: a branch renamed with git branch -m is found again, by its
# worktree, onto a name an archived record holds, which is renamed aside
# (4c32119c, the rc1 full stage).
#
# The name renamed onto is the run's own (run_name), and the row archives a
# branch of that name first, so the archived record holds it: the fixed
# dockhand/ds5-after was a Git branch an earlier run left, and git branch -m
# refused it (the rc4 batch).
act() {
	DS5_AFTER=$(run_name ds5-after)
	dh_setup start "$DS5_AFTER" --port "${ACCEPT_GO_PORT:?}" || return 0
	allow_ref_gone "refs/heads/dockhand/$DS5_AFTER"
	dh archive "$DS5_AFTER" </dev/null || return 0
	# Archiving keeps the record; a Git branch it kept would refuse the
	# rename, which is Git's rule, not what the row tests.
	git -C "$MACPORTS_TREE" branch -D "dockhand/$DS5_AFTER" >/dev/null 2>&1 || :
	dh_setup start $(run_name ds5-before) --port "${ACCEPT_GO_PORT:?}" || return 0
	allow_ref_gone refs/heads/dockhand/$(run_name ds5-before)
	git -C "$MACPORTS_TREE" branch -m dockhand/$(run_name ds5-before) "dockhand/$DS5_AFTER"
	next_superseded "$(run_name ds5-before) renamed $DS5_AFTER"
	DS5_DIR=$("$DH_BIN" path $(run_name ds5-before) 2>/dev/null || :)
	dh_json status || :
}
assert() {
	if jq -e --arg b "dockhand/${DS5_AFTER:-}" '.result.branches[]? | select(.git_branch == $b)' "$ROW_DIR/json/1.json" >/dev/null; then
		row_pass "status follows the renamed branch onto the archived record's name"
	else
		row_fail "status lost the renamed branch: $(jq -c '[.result.branches[]? | {name, git_branch, missing}]' "$ROW_DIR/json/1.json")"
	fi
}
