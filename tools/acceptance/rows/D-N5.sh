# stages: full
# D-N5: upstream retags a release between update and check. A Git-fetched
# port is out of scope, so a branch whose tag moved is planned with
# --plan: the plan and submit's preview say the check built another
# source.
#
# It needs a branch whose Git tag moved upstream after its check, named as
# ACCEPT_DN5_BRANCH: the run can't move a tag it doesn't own, and a test
# repository of the account's own goes past what the person has allowed.
# Without one, the row isn't run (the rc6 full stage).
act() {
	host_only "a branch whose tag moved upstream" || return 0
	if [ -z "${ACCEPT_DN5_BRANCH:-}" ]; then
		row_result "not run" "no branch whose Git tag moved upstream after its check was named as ACCEPT_DN5_BRANCH"
		return 0
	fi
	dh_json check -b "$ACCEPT_DN5_BRANCH" --plan || :
	dh_json submit -b "$ACCEPT_DN5_BRANCH" --plan || :
}
assert() {
	grep -qiE 'moved|another (source|commit)' "$ROW_DIR/out.log" "$ROW_DIR"/json/*.json &&
		row_pass "the plan and preview said the source moved" || row_fail "nothing said the tag moved"
}
