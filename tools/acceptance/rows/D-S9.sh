# stages: quick full
# D-S9: the person's own Git runs while dockhand works: git gc --prune=now
# and git fetch --prune in the main checkout during a check, and a tracked
# branch deleted with git branch -D. dockhand carries on or says what
# moved, and no work is lost (H1).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	dh_bg check -p "$(port)"
	wait_for_line "$DH_BG_LOG" 'building in' 900 || :
	git -C "$MACPORTS_TREE" gc -q --prune=now 2>>"$ROW_DIR/git.log" || :
	# The prune is the row's own, the person's: the remote-tracking refs it
	# removes for fork branches gone are no work dockhand lost (the rc6
	# full stage, where H1 counted two).
	allow_ref_gone 'refs/remotes/origin/*'
	git -C "$MACPORTS_TREE" fetch -q --prune origin 2>>"$ROW_DIR/git.log" || :
	dh_bg_wait || :
	echo "$?" >"$ROW_DIR/ds9.check"
	dh_setup start ds9-gone || return 0
	allow_ref_gone refs/heads/dockhand/ds9-gone
	git -C "$MACPORTS_TREE" worktree remove --force "$("$DH_BIN" path ds9-gone)" 2>>"$ROW_DIR/git.log" || :
	git -C "$MACPORTS_TREE" branch -D dockhand/ds9-gone >>"$ROW_DIR/git.log" 2>&1 || :
	next_superseded "ds9-gone deleted"
	dh_json status ds9-gone || :
}
assert() {
	case "$(cat "$ROW_DIR/ds9.check" 2>/dev/null)" in
	0 | 2) ;;
	*) row_fail "the check under the person's gc and fetch ended $(cat "$ROW_DIR/ds9.check" 2>/dev/null)"; return ;;
	esac
	if ! jq -e '(.result.branches[0].missing // false) == true' "$ROW_DIR/json/1.json" >/dev/null; then
		row_fail "status of a branch deleted with git branch -D didn't say it's gone: $(head -c 300 "$ROW_DIR/json/1.json")"
		return
	fi
	row_pass "the check carried on through gc and fetch; status says the deleted branch is gone"
}
