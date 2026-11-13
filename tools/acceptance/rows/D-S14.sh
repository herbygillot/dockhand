# stages: quick full
# D-S14: two databases on one checkout don't take each other's checkpoints
# (826ef485, rc2's full stage). The checkout already has the checkpoint
# ref this database's next tidy would name, tidy-N, as another database's
# tidy leaves it; this database's tidy names its checkpoint tidy-N+1,
# skipping the name, and leaves the other's ref where it was, where it
# took the name and moved it, or was refused. N is read from the
# database: dhtest's is past tidy-900, so a planted tidy-1 tested no
# collision there (rc4's full stage).

# ds14_next is the checkpoint number this database's next tidy takes, as
# the store gives it (NextCheckpointNumber), 1 for a database not made yet.
ds14_next() {
	local common
	common=$(git -C "${MACPORTS_TREE:?}" rev-parse --path-format=absolute --git-common-dir) || return 1
	if [ ! -f "${DOCKHAND_DB:?}" ]; then
		printf '1'
		return
	fi
	# A copy, as stage_db reads one: sqlite3 -readonly couldn't open the
	# WAL database with no -shm beside it, "unable to open database file
	# (14)" (rc4's reruns).
	stage_db "SELECT coalesce(max(c.number), 0) + 1 FROM checkpoints c JOIN repositories r ON r.id = c.repository_id WHERE r.common_dir = '$common'"
}

setup() {
	DS14_BRANCH=$(run_name ds14)
	DS14_N=$(ds14_next) || return 1
	[ -n "$DS14_N" ] || return 1
	git -C "${MACPORTS_TREE:?}" update-ref "refs/dockhand/checkpoints/tidy-$DS14_N" master || return 1
	DS14_OTHER=$(git -C "$MACPORTS_TREE" rev-parse "refs/dockhand/checkpoints/tidy-$DS14_N")
	dh_setup start "$DS14_BRANCH" --port "${ACCEPT_GO_PORT:?}" </dev/null || return 1
	dh_setup revbump "$ACCEPT_GO_PORT" -b "$DS14_BRANCH" --subject "rebuild for the row" </dev/null || return 1
}
act() {
	[ -n "${DS14_OTHER:-}" ] || return 0
	allow_change "$("$DH_BIN" path "$DS14_BRANCH")/*"
	dh_json tidy -b "$DS14_BRANCH" -y </dev/null || :
}
assert() {
	local json=$ROW_DIR/json/1.json want=tidy-$((DS14_N + 1))
	[ -f "$json" ] || { row_fail "tidy didn't run"; return; }
	if [ "$(cat "$json.exit")" != 0 ]; then
		row_fail "tidy, with another database's tidy-$DS14_N in the checkout, exited $(cat "$json.exit"): $(jq -r '.error // empty' "$json")"
		return
	fi
	if [ "$(jq -r '.result.applied.checkpoint // empty' "$json")" != "$want" ]; then
		row_fail "tidy's checkpoint is $(jq -r '.result.applied.checkpoint // "none"' "$json"), not $want"
		return
	fi
	if [ "$(git -C "$MACPORTS_TREE" rev-parse "refs/dockhand/checkpoints/tidy-$DS14_N" 2>/dev/null)" != "$DS14_OTHER" ]; then
		row_fail "the other database's tidy-$DS14_N moved or went"
		return
	fi
	git -C "$MACPORTS_TREE" rev-parse -q --verify "refs/dockhand/checkpoints/$want" >/dev/null || { row_fail "tidy said $want, and the checkout has no such ref"; return; }
	row_pass "tidy's checkpoint is $want, the other database's tidy-$DS14_N left where it was"
}
