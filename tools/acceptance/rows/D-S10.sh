# stages: full
# D-S10: another contributor's PR lands the same update first: status
# says the branch landed by another route and offers archive; nothing is
# pushed over it.
#
# The row lands it itself, on a master of its own: a bare repository at
# MacPorts' master, borrowing the ports clone's objects as C4's does,
# named by DOCKHAND_UPSTREAM for the row alone. Once the branch is made
# and tidied, that master moves to the branch's commit, standing in for
# the other PR, and a plan fetches it, as status reads the master last
# fetched. Nobody lands MacPorts' updates for a run (the rc6 full stage).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "an update landed by someone else" || return 0
	local objects master
	objects=$(git -C "${MACPORTS_TREE:?}" rev-parse --path-format=absolute --git-common-dir)/objects
	master=$(git -C "$MACPORTS_TREE" rev-parse upstream/master 2>/dev/null || git -C "$MACPORTS_TREE" rev-parse origin/master) || return 1
	git init -q --bare -b master "$ROW_DIR/upstream.git" || return 1
	printf '%s\n' "$objects" >"$ROW_DIR/upstream.git/objects/info/alternates"
	git -C "$ROW_DIR/upstream.git" update-ref refs/heads/master "$master" || return 1
	export DOCKHAND_UPSTREAM="$ROW_DIR/upstream.git"
	dh_setup update "$(port)" --new || return 1
	DS10_BRANCH=$(own_branch)
	dh tidy -b "$DS10_BRANCH" -y || return 1
	# The other PR lands: master at the branch's own commit.
	git -C "$ROW_DIR/upstream.git" fetch -q "$MACPORTS_TREE" "dockhand/$DS10_BRANCH" || return 1
	git -C "$ROW_DIR/upstream.git" update-ref refs/heads/master FETCH_HEAD || return 1
}
act() {
	host_only "an update landed by someone else" || return 0
	[ -n "${DS10_BRANCH:-}" ] || return 0
	# A plan fetches master, and changes nothing.
	dh update "$(port)" --new --plan || :
	dh_json status "$DS10_BRANCH" || :
	dh status || :
}
assert() {
	local status
	status=$(grep -l "^status $DS10_BRANCH$" "$ROW_DIR"/json/*.json.args 2>/dev/null | head -1)
	jq -e '.result.branches[0].on_master // empty' "${status%.args}" >/dev/null ||
		{ row_fail "status didn't say it landed elsewhere"; return; }
	grep -q "its changes are on master already" "$ROW_DIR/out.log" && grep -q "dockhand archive $DS10_BRANCH" "$ROW_DIR/out.log" ||
		{ row_fail "status didn't offer archive"; return; }
	row_pass "status said it landed by another route, and offered archive"
}
