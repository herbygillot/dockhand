# stages: full
# D-S1: someone pushes to the fork branch after submit: the next submit
# refuses rather than overwriting (H2), and its preview's Push line says
# it won't push.
#
# The row makes its test pull request and the outside push in setup,
# before the harm sweep's snapshot: a person made them inside it, where H2
# and H3 took them for the row's harm (the rc6 full stage). The outside
# push is a commit made in the worktree, pushed to the fork's branch, and
# taken back out of the worktree, so dockhand's branch is behind it.
# prs: ${ACCEPT_RUST_PORT} test
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
setup() {
	host_only "a submitted branch on the fork" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull request for $(port) isn't an approved test one, and it closes what it opens"
		return 0
	fi
	dh_setup update "$(port)" --new || return 1
	DS1_BRANCH=$(own_branch)
	dh tidy -b "$DS1_BRANCH" -y || return 1
	submit_pr "$(port)" "$DS1_BRANCH" --no-check || return 1
	local dir
	dir=$(dh_quiet path "$DS1_BRANCH") || return 1
	git -C "$dir" -c user.name=Someone -c user.email=someone@example.invalid commit -q --allow-empty -m "someone else's commit" || return 1
	git -C "$dir" push -q origin "HEAD:refs/heads/dockhand/$DS1_BRANCH" >>"$ROW_DIR/out.log" 2>&1 || return 1
	git -C "$dir" reset -q --hard HEAD~1 || return 1
	# A commit of dockhand's own on top, so there's something to push.
	printf '# a later edit\n' >>"$dir/$(dh_quiet --json status "$DS1_BRANCH" | jq -r '.result.branches[0].directories[0]')/Portfile"
	dh tidy -b "$DS1_BRANCH" -y || return 1
}
act() {
	host_only "a submitted branch on the fork" || return 0
	[ -n "${DS1_BRANCH:-}" ] || return 0
	dh submit -b "$DS1_BRANCH" --plan || :
	dh_json submit -b "$DS1_BRANCH" -y || :
	close_test_pr "$(port)" "$DS1_BRANCH"
}
assert() {
	local submit
	submit=$(grep -l '^submit .* -y$' "$ROW_DIR"/json/*.json.args 2>/dev/null | tail -1)
	[ "$(cat "${submit%.args}.exit" 2>/dev/null)" != 0 ] || { row_fail "submit went ahead over someone's push"; return; }
	grep -q "Push .*won't push: someone else pushed" "$ROW_DIR/out.log" ||
		{ row_fail "the preview's Push line didn't say it won't push"; return; }
	row_pass "refused: $(jq -r '.error // empty' "${submit%.args}")"
}
