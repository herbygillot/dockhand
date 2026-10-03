# stages: quick full
# D-S3: a worktree deleted by hand, with rm -rf and with git worktree
# remove: status says so, path checks it out again, and the branch's
# record is intact.
act() {
	dh_setup start ds3-rm --port "${ACCEPT_GO_PORT:?}" || return 0
	dh_setup start ds3-git --port "${ACCEPT_GO_PORT}" || return 0
	local rm_dir git_dir
	rm_dir=$("$DH_BIN" path ds3-rm) && git_dir=$("$DH_BIN" path ds3-git) || return 0
	allow_change "$rm_dir/*" "$git_dir/*"
	rm -rf "$rm_dir"
	git -C "$MACPORTS_TREE" worktree remove --force "$git_dir"
	dh_json status ds3-rm || :
	dh_json status ds3-git || :
	dh path ds3-rm || :
	dh path ds3-git || :
	DS3_RM=$rm_dir DS3_GIT=$git_dir
}
assert() {
	local file
	for file in "$ROW_DIR/json/1.json" "$ROW_DIR/json/2.json"; do
		[ "$(cat "$file.exit")" = 0 ] || { row_fail "status of a branch whose worktree went exited $(cat "$file.exit"): $(jq -r '.error // empty' "$file")"; return; }
	done
	[ -d "$DS3_RM" ] && [ -d "$DS3_GIT" ] || { row_fail "path didn't check the worktrees out again"; return; }
	row_pass "status read both, and path checked both out again"
}
