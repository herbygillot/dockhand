# stages: quick full
# D-S13: clean --delete keeps a worktree holding work of its own. An
# archived branch whose worktree was kept, with an untracked file in it,
# isn't forgotten: clean --delete refuses, exit 3, naming the file, and the file
# and the record stay, where its forced removal lost them with no word
# (Herby's test-suite analysis, 2026-10-31). With the file gone, it's
# forgotten.
setup() {
	DS13_BRANCH=$(run_name ds13)
	dh_setup start "$DS13_BRANCH" </dev/null || return 1
	DS13_WORKTREE=$("$DH_BIN" path "$DS13_BRANCH") || return 1
	dh_setup archive "$DS13_BRANCH" --keep-worktree </dev/null || return 1
	printf 'mine\n' >"$DS13_WORKTREE/ds13-notes.txt"
	# The file goes in act, and the branch once it's forgotten.
	allow_change "$DS13_WORKTREE/*"
	allow_ref_gone "*/$DS13_BRANCH"
}
act() {
	[ -n "${DS13_WORKTREE:-}" ] || return 0
	dh_json clean --delete "$DS13_BRANCH" -y </dev/null || :
	[ -f "$DS13_WORKTREE/ds13-notes.txt" ] && echo kept >"$ROW_DIR/notes-kept"
	rm -f "$DS13_WORKTREE/ds13-notes.txt"
	dh_json clean --delete "$DS13_BRANCH" -y </dev/null || :
}
assert() {
	local refused=$ROW_DIR/json/1.json forgot=$ROW_DIR/json/2.json
	[ -f "$refused.exit" ] || { row_fail "clean --delete never ran"; return; }
	if [ "$(cat "$refused.exit")" = 0 ]; then
		row_fail "clean --delete forgot a branch whose worktree held an untracked file"
		return
	fi
	if [ "$(cat "$refused.exit")" != 3 ]; then
		row_fail "clean --delete refused, exit $(cat "$refused.exit"), not 3: $(jq -r '.error // empty' "$refused")"
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
	row_pass "refused while the worktree held an untracked file, keeping it: $(jq -r '.error' "$refused"); forgot it once the file was gone"
}
