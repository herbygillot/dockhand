# stages: full
# D-I4: kill -9 during submit, after the push and before the PR opens,
# the release binary killed when -v names the step. Re-running finds or
# opens exactly one PR (H3).
# prs: ${ACCEPT_RUST_PORT} test
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	host_only "a pull request on GitHub" || return 0
	pr_approved "$(port)" || { row_result "not run" "its pull request wasn't approved"; return 0; }
	dh update "$(port)" --new || return 0
	DI4_BRANCH=$(dh_quiet --json status --port "$(port)" | jq -r '.result.branches[0].name')
	dh check -b "$DI4_BRANCH" && dh tidy -b "$DI4_BRANCH" -y || return 0
	allow_push "*$DI4_BRANCH*"
	allow_prs 1
	dh_bg -v submit -b "$DI4_BRANCH" -y --title "[testing] $(port)" --skip-notification --note "This pull request tests a dockhand release candidate and will be closed."
	wait_for_line "$DH_BG_LOG" 'opening pull request' 600 && kill -9 "$DH_BG_PID"
	dh_bg_wait || :
	dh_json submit -b "$DI4_BRANCH" -y || :
	close_test_pr "$(port)" "$DI4_BRANCH"
}
assert() {
	grep -q '^\[exit 137\]' "$ROW_DIR/out.log" || { row_fail "submit wasn't killed at opening the pull request"; return; }
	[ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ] || { row_fail "the submit after the kill failed: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"; return; }
	row_pass "the submit after the kill found or opened the one pull request (H3 counts)"
}
