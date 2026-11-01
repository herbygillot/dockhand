# stages: quick full
# D-S12: a branch moved onto a newer master with Git, behind dockhand's
# back, as a person's own git rebase moves it, or another database's
# dockhand on the same checkout: its commits since the base dockhand
# records include master's, so submit's preview refuses, exit 3, naming
# dockhand rebase, where it titled and sent master's commits as the
# branch's (rc2's full stage, A4: "lang/sbcl: update to sbcl-2.6.10").
# dockhand rebase records the base anew, and the preview goes on, titled
# for the port's own commit.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
setup() {
	# The full stage has no pinned upstream, only MacPorts' own (the M1's
	# rc6 full stage, where the row gave no result): there it makes one
	# of its own, as D-S10 does, a commit behind MacPorts' master, so it
	# has a commit to move on to, and the person's fetch reads it.
	DS12_FETCH=origin
	if [ -z "${DOCKHAND_UPSTREAM:-}" ]; then
		local objects master
		objects=$(git -C "${MACPORTS_TREE:?}" rev-parse --path-format=absolute --git-common-dir)/objects
		master=$(git -C "$MACPORTS_TREE" rev-parse upstream/master 2>/dev/null || git -C "$MACPORTS_TREE" rev-parse origin/master) || return 1
		git init -q --bare -b master "$ROW_DIR/upstream.git" || return 1
		printf '%s\n' "$objects" >"$ROW_DIR/upstream.git/objects/info/alternates"
		git -C "$ROW_DIR/upstream.git" update-ref refs/heads/master "$master^" || return 1
		export DOCKHAND_UPSTREAM="$ROW_DIR/upstream.git"
		DS12_FETCH=$DOCKHAND_UPSTREAM DS12_NEXT=$master
	fi
	dh_setup update "$(port)" --new || return 1
	DS12_BRANCH=$(own_branch)
	dh tidy -b "$DS12_BRANCH" -y || return 1
	# The pinned upstream moves on one commit, as D-I3 moves it, and is
	# put back in teardown.
	DS12_MASTER=$(git -C "$DOCKHAND_UPSTREAM" rev-parse master)
	[ -n "${DS12_NEXT:-}" ] || DS12_NEXT=$(git -C "$DOCKHAND_UPSTREAM" rev-list --ancestry-path --reverse master..$(git -C "${ACCEPT_PORTS_SOURCE:-$HOME/Source/macports-ports}" rev-parse origin/master) 2>/dev/null | head -1)
	git -C "$DOCKHAND_UPSTREAM" update-ref refs/heads/master "$DS12_NEXT" || return 1
}
teardown() {
	[ -z "${DS12_MASTER:-}" ] || git -C "$DOCKHAND_UPSTREAM" update-ref refs/heads/master "$DS12_MASTER"
	return 0
}
act() {
	[ -n "${DS12_BRANCH:-}" ] || return 0
	local worktree
	worktree=$("$DH_BIN" path "$DS12_BRANCH") || return 0
	# The person's own fetch and rebase.
	if [ "$DS12_FETCH" = origin ]; then
		{ git -C "$worktree" fetch -q origin && git -C "$worktree" rebase -q origin/master; } >>"$ROW_DIR/out.log" 2>&1 || return 0
	else
		{ git -C "$worktree" fetch -q "$DS12_FETCH" master && git -C "$worktree" rebase -q FETCH_HEAD; } >>"$ROW_DIR/out.log" 2>&1 || return 0
	fi
	dh_json submit -b "$DS12_BRANCH" --plan || :
	dh_json rebase -b "$DS12_BRANCH" || :
	dh_json submit -b "$DS12_BRANCH" --plan || :
}
assert() {
	local refused=$ROW_DIR/json/1.json again=$ROW_DIR/json/3.json title
	[ -f "$refused.exit" ] || { row_fail "submit's preview of the moved branch never ran"; return; }
	if [ "$(cat "$refused.exit")" != 3 ]; then
		row_fail "submit's preview of a branch carrying master's commits exited $(cat "$refused.exit"), not 3: $(jq -r '.error // empty' "$refused")"
		return
	fi
	if ! grep -qE "dockhand rebase (-b|--branch) $DS12_BRANCH" "$ROW_DIR/out.log"; then
		row_fail "the refusal didn't name dockhand rebase: $(jq -r '.error // empty' "$refused")"
		return
	fi
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 0 ] || { row_fail "rebase exited $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"; return; }
	title=$(jq -r '.result.title // empty' "$again")
	case "$title" in
	"$(port): "*) row_pass "refused the moved branch, naming dockhand rebase; after it, the preview is titled \"$title\"" ;;
	*) row_fail "after rebase, the preview's title is \"$title\", not $(port)'s own commit's (exit $(cat "$again.exit" 2>/dev/null): $(jq -r '.error // empty' "$again"))" ;;
	esac
}
