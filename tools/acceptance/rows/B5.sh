# stages: full
# B5: rebase onto a newer master, then submit again: the checkpoint is
# kept, a check that still applies is reused, and the PR is updated.
# prs: ${ACCEPT_RUST_PORT} test
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	host_only "a pull request on GitHub" || return 0
	dh_setup update "$(port)" --new || return 0
	B5_BRANCH=$(dh_quiet --json status --port "$(port)" | jq -r '.result.branches[0].name')
	dh check -b "$B5_BRANCH" && dh tidy -b "$B5_BRANCH" -y || return 0
	submit_pr "$(port)" "$B5_BRANCH" || return 0
	checkpoint "wait until MacPorts' master has moved past the branch's base" || return 0
	dh_json rebase -b "$B5_BRANCH" || :
	# A rebase moves the commit, so submit wants a check of the new one,
	# which reuses the earlier build where the port reads the same files:
	# without it, submit refused (the rc6 full stage).
	dh_json check -b "$B5_BRANCH" || :
	dh_json submit -b "$B5_BRANCH" -y || :
	close_test_pr "$(port)" "$B5_BRANCH"
}
assert() {
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 0 ] || { row_fail "rebase failed: $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"; return; }
	jq -e '[.result.targets[]?.results[]?.reused_from // empty] | length > 0' "$ROW_DIR/json/3.json" >/dev/null ||
		{ row_fail "the check after the rebase reused nothing: $(jq -c '[.result.targets[]?.results[]? | {outcome, reused_from}]' "$ROW_DIR/json/3.json" 2>/dev/null)"; return; }
	[ "$(cat "$ROW_DIR/json/4.json.exit" 2>/dev/null)" = 0 ] || { row_fail "the submit after the rebase failed: $(jq -r '.error // empty' "$ROW_DIR/json/4.json")"; return; }
	judged "the rebase kept the checkpoint, the check was reused, and the PR was updated in place"
}
