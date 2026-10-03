# stages: full
# D-N5: upstream retags a release between update and check. A Git-fetched
# port is out of scope, so a branch whose tag moved is planned with
# --plan: the plan and submit's preview say the check built another
# source.
act() {
	host_only "a branch whose tag moved upstream" || return 0
	checkpoint "name a branch of this run whose Git tag moved upstream after its check in $ROW_DIR/branch" || return 0
	dh_json check -b "$(cat "$ROW_DIR/branch")" --plan || :
	dh_json submit -b "$(cat "$ROW_DIR/branch")" --plan || :
}
assert() {
	grep -qiE 'moved|another (source|commit)' "$ROW_DIR/out.log" "$ROW_DIR"/json/*.json &&
		row_pass "the plan and preview said the source moved" || row_fail "nothing said the tag moved"
}
