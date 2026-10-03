# stages: full
# D-C1: no login at all: read-only commands work, and submit refuses,
# naming auth login.
act() {
	host_only "the test user's own Keychain" || return 0
	dh auth logout || :
	dh_json status || :
	dh_json outdated --mine || :
	dh_json submit --plan || :
	checkpoint "log back in: dockhand auth login, as the test account" || return 0
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ] || { row_fail "status failed without a login"; return; }
	jq -r '.error // empty' "$ROW_DIR/json/3.json" | grep -q 'auth login' &&
		row_pass "read-only commands worked; submit named auth login" || row_fail "submit didn't name auth login"
}
