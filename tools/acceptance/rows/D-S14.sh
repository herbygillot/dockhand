# stages: quick full
# D-S14: two databases on one checkout don't take each other's checkpoints
# (826ef485, rc2's full stage). The checkout already has
# refs/dockhand/checkpoints/tidy-1, as another database's tidy leaves it;
# this database's first tidy names its checkpoint tidy-2, skipping the
# name, and leaves the other's ref where it was, where it took the name
# and moved it, or was refused.
setup() {
	DS14_BRANCH=$(run_name ds14)
	git -C "${MACPORTS_TREE:?}" update-ref refs/dockhand/checkpoints/tidy-1 master || return 1
	DS14_OTHER=$(git -C "$MACPORTS_TREE" rev-parse refs/dockhand/checkpoints/tidy-1)
	dh_setup start "$DS14_BRANCH" --port "${ACCEPT_GO_PORT:?}" </dev/null || return 1
	dh_setup revbump "$ACCEPT_GO_PORT" -b "$DS14_BRANCH" --subject "rebuild for the row" </dev/null || return 1
}
act() {
	[ -n "${DS14_OTHER:-}" ] || return 0
	allow_change "$("$DH_BIN" path "$DS14_BRANCH")/*"
	dh_json tidy -b "$DS14_BRANCH" -y </dev/null || :
}
assert() {
	local json=$ROW_DIR/json/1.json
	[ -f "$json" ] || { row_fail "tidy didn't run"; return; }
	if [ "$(cat "$json.exit")" != 0 ]; then
		row_fail "tidy, with another database's tidy-1 in the checkout, exited $(cat "$json.exit"): $(jq -r '.error // empty' "$json")"
		return
	fi
	if [ "$(jq -r '.result.applied.checkpoint // empty' "$json")" != tidy-2 ]; then
		row_fail "tidy's checkpoint is $(jq -r '.result.applied.checkpoint // "none"' "$json"), not tidy-2"
		return
	fi
	if [ "$(git -C "$MACPORTS_TREE" rev-parse refs/dockhand/checkpoints/tidy-1 2>/dev/null)" != "$DS14_OTHER" ]; then
		row_fail "the other database's tidy-1 moved or went"
		return
	fi
	git -C "$MACPORTS_TREE" rev-parse -q --verify refs/dockhand/checkpoints/tidy-2 >/dev/null || { row_fail "tidy said tidy-2, and the checkout has no such ref"; return; }
	row_pass "tidy's checkpoint is tidy-2, the other database's tidy-1 left where it was"
}
