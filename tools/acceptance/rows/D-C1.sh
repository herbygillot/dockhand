# stages: full
# D-C1: no login at all: read-only commands work, and submit refuses,
# naming auth login.
#
# Its submit names a branch of its own, made while logged in: on the
# ports checkout's master, with earlier rows' branches set aside, submit
# refused for want of a branch before it asked for a login (the rc6 full
# stage).
#
# dhtest's GitHub CLI is the test account's too, which dockhand takes in
# the Keychain's place, so the row sets the CLI's login aside as well, as
# D-C2 does, held in a shell variable, and the token variables are unset
# for the commands it runs logged out; and its submit is --no-check, so
# the missing login is all that's left to hold it. With the CLI's login
# in place submit found the account, and held only for want of a check
# (the rc8 full stage).
DC1_HOSTS=$HOME/.config/gh/hosts.yml
DC1_GH=""

# dc1_gh_back puts the CLI's login back, once.
dc1_gh_back() {
	[ -n "$DC1_GH" ] || return 0
	printf '%s\n' "$DC1_GH" >"$DC1_HOSTS" && chmod 600 "$DC1_HOSTS"
	DC1_GH=""
}
setup() {
	host_only "the test user's own Keychain" || return 0
	dh_setup update "${ACCEPT_GO_PORT:?}" --new || return 1
	DC1_BRANCH=$(own_branch)
	dh tidy -b "$DC1_BRANCH" -y || return 1
}
act() {
	host_only "the test user's own Keychain" || return 0
	login_keep || { row_result "not run" "dockhand has no login in the Keychain to log out of"; return 0; }
	dh auth logout || :
	[ -f "$DC1_HOSTS" ] && DC1_GH=$(cat "$DC1_HOSTS") && rm -f "$DC1_HOSTS"
	(
		unset GH_TOKEN GITHUB_TOKEN
		dh_json status || :
		dh_json outdated --mine || :
		dh_json submit -b "$DC1_BRANCH" --plan --no-check || :
	)
	dc1_gh_back
	# The login comes back as it was, with no code to enter.
	login_restore || device_login "D-C1 logs dockhand back in" || return 0
}
# The CLI's login comes back however act ended.
teardown() {
	dc1_gh_back
	return 0
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ] || { row_fail "status failed without a login"; return; }
	jq -r '.error // empty' "$ROW_DIR/json/3.json" | grep -q 'auth login' &&
		row_pass "read-only commands worked; submit named auth login" || row_fail "submit didn't name auth login"
}
