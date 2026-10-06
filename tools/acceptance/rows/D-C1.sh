# stages: full
# D-C1: no login at all: read-only commands work, and submit refuses,
# naming auth login.
#
# Its submit names a branch of its own, made while logged in: on the
# ports checkout's master, with earlier rows' branches set aside, submit
# refused for want of a branch before it asked for a login (the rc6 full
# stage).
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
	dh_json status || :
	dh_json outdated --mine || :
	dh_json submit -b "$DC1_BRANCH" --plan || :
	# The login comes back as it was, with no code to enter.
	login_restore || device_login "D-C1 logs dockhand back in" || return 0
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ] || { row_fail "status failed without a login"; return; }
	jq -r '.error // empty' "$ROW_DIR/json/3.json" | grep -q 'auth login' &&
		row_pass "read-only commands worked; submit named auth login" || row_fail "submit didn't name auth login"
}
