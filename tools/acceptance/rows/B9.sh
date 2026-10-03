# stages: full
# B9: the Intel user's path, on Apple silicon with no Tart set up: with
# check.on = ["github"], update, check, tidy and submit. providers and
# check say there's no on-host provider and point to --on github; the
# check runs MacPorts' workflow in the fork and reads back; the
# dockhand-check/ branch is removed.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a fork with Actions, before Tart's setup" || return 0
	allow_change "*"
	allow_push "*dockhand-check/*"
	dh providers || :
	printf '\n[check]\non = ["github"]\n' >>"${DOCKHAND_CONFIG:-$HOME/.dockhand/config.toml}"
	dh_setup update "$(port)" --new || return 0
	B9_BRANCH=$(dh_quiet --json status --port "$(port)" | jq -r '.result.branches[0].name')
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
	judged "providers said there's no on-host provider and pointed to --on github"
}
