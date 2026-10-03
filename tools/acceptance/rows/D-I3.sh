# stages: quick
# D-I3: kill -9 during tidy and during rebase, between the checkpoint and
# the branch's move, with the acceptance build's failpoints. The next
# tidy, rebase, or undo finishes or drops what was left, as usage.md says,
# and no work is lost (H1).
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	dh update "$(port)" --new || return 0
	local branch
	branch=$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*' --format='%(refname:short)' | head -1)
	DI3_BRANCH=${branch#dockhand/}
	DOCKHAND_FAILPOINT=tidy.prepared:kill DH_BIN="${DH_FAILPOINT_BIN:?}" dh tidy -b "$DI3_BRANCH" -y || :
	dh_json status "$DI3_BRANCH" || :
	dh_json tidy -b "$DI3_BRANCH" -y || :
	# The pinned upstream moves on a commit, so the rebase has somewhere
	# to go.
	git -C "$DOCKHAND_UPSTREAM" update-ref refs/heads/master "$(git -C "$DOCKHAND_UPSTREAM" rev-list -1 --ancestry-path --reverse master..$(git -C "${ACCEPT_PORTS_SOURCE:-$HOME/Source/macports-ports}" rev-parse origin/master) 2>/dev/null | head -1)" 2>/dev/null || :
	DOCKHAND_FAILPOINT=rebase.prepared:kill DH_BIN="$DH_FAILPOINT_BIN" dh rebase -b "$DI3_BRANCH" || :
	dh_json rebase -b "$DI3_BRANCH" || :
}
assert() {
	if ! grep -q '^\[exit 137\]' "$ROW_DIR/out.log"; then
		row_fail "no failpoint fired: the kill rows need the acceptance build"
		return
	fi
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 0 ] || { row_fail "tidy after the killed tidy exited $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"; return; }
	case "$(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null)" in
	0) row_pass "the tidy and rebase killed at their checkpoints were finished by the next ones" ;;
	*) row_fail "rebase after the killed rebase exited $(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/3.json")" ;;
	esac
}
