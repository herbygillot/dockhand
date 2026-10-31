# stages: quick full
# D-S13: clean --delete removes only what's dockhand's to remove.
#  - An archived branch whose worktree was kept, with an untracked file in
#    it, isn't forgotten: clean --delete refuses, exit 3, naming the file,
#    and the file and the record stay, where its forced removal lost them
#    with no word (the test-suite analysis, 2026-10-31). With the file
#    gone, it's forgotten.
#  - An archived branch the person checked out in a checkout of their own
#    isn't forgotten either: the refusal, exit 3, names that checkout and
#    git switch, where the branch was deleted from under it. Switched
#    away, it's forgotten, and their checkout stays.
setup() {
	DS13_BRANCH=$(run_name ds13)
	DS13_OTHER=$(run_name ds13-other)
	dh_setup start "$DS13_BRANCH" </dev/null || return 1
	DS13_WORKTREE=$("$DH_BIN" path "$DS13_BRANCH") || return 1
	dh_setup archive "$DS13_BRANCH" --keep-worktree </dev/null || return 1
	printf 'mine\n' >"$DS13_WORKTREE/ds13-notes.txt"
	dh_setup start "$DS13_OTHER" </dev/null || return 1
	dh_setup archive "$DS13_OTHER" </dev/null || return 1
	DS13_MINE=$ROW_DIR/mine
	git -C "$MACPORTS_TREE" worktree add -q "$DS13_MINE" "dockhand/$DS13_OTHER" >>"$ROW_DIR/out.log" 2>&1 || return 1
	# The file goes in act, and the branches once they're forgotten.
	allow_change "$DS13_WORKTREE/*"
	allow_ref_gone "*/$DS13_BRANCH" "*/$DS13_OTHER"
}
teardown() {
	[ -z "${DS13_MINE:-}" ] || git -C "$MACPORTS_TREE" worktree remove --force "$DS13_MINE" 2>/dev/null || :
	return 0
}
act() {
	[ -n "${DS13_MINE:-}" ] || return 0
	dh_json clean --delete "$DS13_BRANCH" -y </dev/null || :
	[ -f "$DS13_WORKTREE/ds13-notes.txt" ] && echo kept >"$ROW_DIR/notes-kept"
	rm -f "$DS13_WORKTREE/ds13-notes.txt"
	dh_json clean --delete "$DS13_BRANCH" -y </dev/null || :
	dh_json clean --delete "$DS13_OTHER" -y </dev/null || :
	git -C "$DS13_MINE" switch -q --detach >>"$ROW_DIR/out.log" 2>&1 || :
	dh_json clean --delete "$DS13_OTHER" -y </dev/null || :
}
assert() {
	local refused=$ROW_DIR/json/1.json forgot=$ROW_DIR/json/2.json held=$ROW_DIR/json/3.json switched=$ROW_DIR/json/4.json
	[ -f "$refused.exit" ] || { row_fail "clean --delete never ran"; return; }
	if [ "$(cat "$refused.exit")" != 3 ]; then
		row_fail "clean --delete of a worktree holding an untracked file exited $(cat "$refused.exit"), not 3: $(jq -r '.error // empty' "$refused")"
		return
	fi
	if ! jq -r '.error // empty' "$refused" | matches 'untracked files: ds13-notes.txt'; then
		row_fail "the refusal didn't name the untracked file: $(jq -r '.error // empty' "$refused")"
		return
	fi
	[ -f "$ROW_DIR/notes-kept" ] || { row_fail "the untracked file went though clean --delete refused"; return; }
	if [ "$(cat "$forgot.exit" 2>/dev/null)" != 0 ]; then
		row_fail "with the file gone, clean --delete exited $(cat "$forgot.exit" 2>/dev/null): $(jq -r '.error // empty' "$forgot")"
		return
	fi
	if [ "$(cat "$held.exit" 2>/dev/null)" != 3 ]; then
		row_fail "clean --delete of a branch checked out in the person's own checkout exited $(cat "$held.exit" 2>/dev/null), not 3: $(jq -r '.error // empty' "$held")"
		return
	fi
	if ! jq -r '.error // empty' "$held" | matches "checked out in .*mine.*git switch"; then
		row_fail "the refusal didn't name the checkout and git switch: $(jq -r '.error // empty' "$held")"
		return
	fi
	if [ "$(cat "$switched.exit" 2>/dev/null)" != 0 ]; then
		row_fail "switched away, clean --delete exited $(cat "$switched.exit" 2>/dev/null): $(jq -r '.error // empty' "$switched")"
		return
	fi
	[ -d "$DS13_MINE" ] || { row_fail "the person's own checkout went with the branch"; return; }
	row_pass "refused while the worktree held an untracked file, keeping it, and while the branch was checked out in the person's own checkout: $(jq -r '.error' "$held"); each forgotten once clear, their checkout kept"
}
