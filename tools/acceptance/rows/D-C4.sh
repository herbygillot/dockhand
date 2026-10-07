# stages: full
# D-C4: GH_TOKEN set to a token for another account: the account it acts
# as is named before any push (H2).
#
# Its submit names a branch of its own, made in setup with the test
# account's login: with earlier rows' branches set aside, submit -p found
# none, and refused before it asked who it acts as (the rc6 full stage).
# The other account's token is read into a variable and its file deleted
# at once, and again in teardown, whatever happened.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	host_only "a second account's token" || return 0
	dh_setup update "$(port)" --new || return 1
	DC4_BRANCH=$(own_branch)
	dh tidy -b "$DC4_BRANCH" -y || return 1
}
act() {
	host_only "a second account's token" || return 0
	[ -n "${DC4_BRANCH:-}" ] || return 0
	checkpoint "put a token for another GitHub account in $ROW_DIR/token: a fine-grained one with no repository access, expiring in a day (the row deletes the file)" || { rm -f "$ROW_DIR/token"; return 0; }
	local token
	token=$(cat "$ROW_DIR/token" 2>/dev/null)
	rm -f "$ROW_DIR/token"
	[ -n "$token" ] || { row_result "not run" "no token was put in $ROW_DIR/token"; return 0; }
	GH_TOKEN=$token "$DH_BIN" auth status >>"$ROW_DIR/out.log" 2>&1 || :
	# --no-check, so the account is all its preview judges: setup makes no
	# check, and the preview held for want of one (the rc8 full stage).
	GH_TOKEN=$token "$DH_BIN" submit -b "$DC4_BRANCH" --plan --no-check >>"$ROW_DIR/out.log" 2>&1 || :
}
teardown() {
	rm -f "$ROW_DIR/token"
	return 0
}
assert() {
	judged "auth status and submit's preview named the other account, and nothing was pushed"
}
