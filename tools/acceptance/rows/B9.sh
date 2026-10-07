# stages: full
# B9: the Intel user's path, on Apple silicon with no Tart set up: with
# check.on = ["github"], update, check, tidy and submit. providers and
# check say there's no on-host provider and point to --on github; the
# check runs MacPorts' workflow in the fork and reads back; the
# dockhand-check/ branch is removed.
# prs: ${ACCEPT_GO_PORT} test
#
# In run order Tart is set up already, A4 and A12 having made images, so
# the row gives dockhand an empty Tart home of its own (DOCKHAND_TART_HOME)
# for "no Tart set up"; and the check.on it sets is the row's alone: its
# setup keeps the config, and its teardown puts it back, which every later
# row's check would otherwise have run on GitHub by (the rc6 full stage).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/config.before"
	mkdir -p "$ROW_DIR/tart-home"
	export DOCKHAND_TART_HOME="$ROW_DIR/tart-home"
}
teardown() {
	[ -f "$ROW_DIR/config.before" ] && cp "$ROW_DIR/config.before" "$DOCKHAND_CONFIG"
	return 0
}
act() {
	host_only "a fork with Actions, before Tart's setup" || return 0
	allow_change "*"
	allow_push "*dockhand-check/*"
	dh providers || :
	printf '\n[check]\non = ["github"]\n' >>"$DOCKHAND_CONFIG"
	dh_setup update "$(port)" --new || return 0
	B9_BRANCH=$(own_branch)
	dh_json check -b "$B9_BRANCH" || :
	dh tidy -b "$B9_BRANCH" -y || :
	submit_pr "$(port)" "$B9_BRANCH" || :
	close_test_pr "$(port)" "$B9_BRANCH"
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ] || { row_fail "the check on GitHub failed: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"; return; }
	if git -C "$MACPORTS_TREE" ls-remote --heads fork 'dockhand-check/*' | grep -q .; then
		row_fail "a dockhand-check/ branch was left on the fork"
		return
	fi
	# On Apple silicon an empty Tart home is Tart not set up yet, and
	# providers points to its setup; only an Intel Mac, where Tart can't
	# run, has no on-host provider to point to (the rc8 full stage).
	if [ "$(uname -m)" = arm64 ]; then
		grep -qE '^ *tart +· not set up: dockhand setup tart' "$ROW_DIR/out.log" ||
			{ row_fail "providers didn't point to dockhand setup tart with no Tart set up: $(grep -i tart "$ROW_DIR/out.log" | head -2 | tr '\n' ' ')"; return; }
		judged "providers pointed to setup tart, and the check ran on GitHub"
	else
		judged "providers said there's no on-host provider and pointed to --on github"
	fi
}
