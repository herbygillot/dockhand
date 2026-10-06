# stages: full
# D-C2: auth logout while serve runs, then the login back: serve follows a
# pull request throughout, saying what it saw, with no restart.
#
# A running serve keeps the login it started with: the client it made
# holds the token, which GitHub honours until it expires or is revoked,
# and pull requests are public, read without a login past that. So a
# logout doesn't stop serve reading, and there's no "can't act" to say;
# the rc6 full stage's serve, with nothing to follow, said nothing at all.
# The row gives it a test pull request in the sandbox to follow, closes it
# while dockhand is logged out, and looks for serve saying so, with the
# same process before and after the login comes back.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "serve and the Keychain" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull request for $(port) isn't an approved test one, and it closes what it opens"
		return 0
	fi
	dh_setup update "$(port)" --new || return 1
	DC2_BRANCH=$(own_branch)
	dh tidy -b "$DC2_BRANCH" -y || return 1
	submit_pr "$(port)" "$DC2_BRANCH" --no-check || return 1
}
act() {
	host_only "serve and the Keychain" || return 0
	[ -n "${DC2_BRANCH:-}" ] || return 0
	login_keep || { row_result "not run" "dockhand has no login in the Keychain to log out of"; return 0; }
	local url log=$HOME/.dockhand/logs/serve.log before
	url=$(dh_quiet --json status "$DC2_BRANCH" | jq -r '.result.branches[0].pull_request.url // empty')
	before=$(wc -c <"$log" 2>/dev/null | tr -d ' ' || echo 0)
	dh serve --install || return 0
	sleep 20
	dh_quiet --json queue | jq -r '.result.serve_state.pid // empty' >"$ROW_DIR/pid.before"
	dh auth logout || :
	gh pr close "$url" >>"$ROW_DIR/out.log" 2>&1 || :
	# serve reads pull requests every five minutes.
	sleep 360
	login_restore || device_login "D-C2 logs dockhand back in while serve runs" || return 0
	sleep 60
	dh_quiet --json queue | jq -r '.result.serve_state.pid // empty' >"$ROW_DIR/pid.after"
	tail -c +"$((${before:-0} + 1))" "$log" >"$ROW_DIR/serve.log" 2>/dev/null || :
	dh serve --uninstall || :
	allow_change "*"
	allow_ref_gone "*"
	dh_json clean --closed -y || :
}
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}
assert() {
	grep -qE "#[0-9]+ is closed" "$ROW_DIR/serve.log" ||
		{ row_fail "serve didn't say the pull request closed while dockhand was logged out: $(tail -3 "$ROW_DIR/serve.log" | tr '\n' ';')"; return; }
	if [ ! -s "$ROW_DIR/pid.after" ] || ! cmp -s "$ROW_DIR/pid.before" "$ROW_DIR/pid.after"; then
		row_fail "serve didn't keep running across the logout and the login: pid $(cat "$ROW_DIR/pid.before") before, $(cat "$ROW_DIR/pid.after" 2>/dev/null) after"
		return
	fi
	row_pass "serve followed the pull request through the logout, and ran on with the login back, as one process"
}
