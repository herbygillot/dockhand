# stages: full
# D-C5: only the GitHub CLI's login: it's used, and setup says it's
# the CLI's.
act() {
	host_only "the GitHub CLI's login" || return 0
	login_keep || { row_result "not run" "dockhand has no login in the Keychain to log out of"; return 0; }
	dh setup github --logout || :
	# The CLI is the test account's already, as reset-user.sh logs it in.
	if [ "$(gh api user --jq .login 2>/dev/null)" != "${ACCEPT_GH_LOGIN:-}" ]; then
		checkpoint "log the GitHub CLI in as the test account: gh auth login" || { login_restore; return 0; }
	fi
	dh_json setup || :
	login_restore || device_login "D-C5 logs dockhand back in" || return 0
}
assert() {
	jq -e '.result.github.source == "GitHub CLI"' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "setup said it's the CLI's login" || row_fail "setup didn't name the CLI: $(jq -c '.result.github // .error' "$ROW_DIR/json/1.json" 2>/dev/null)"
}
