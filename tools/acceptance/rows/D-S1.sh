# stages: full
# D-S1: someone pushes to the fork branch after submit: the next submit
# refuses rather than overwriting (H2), and its preview's Push line says
# it won't push.
#
# The row makes its test pull request and the outside push in setup,
# before the harm sweep's snapshot: a person made them inside it, where H2
# and H3 took them for the row's harm (the rc6 full stage). The outside
# push is a commit made in a clone of the fork's branch of its own, as a
# collaborator's is, and pushed there, so dockhand's branch is behind it.
# Made in the worktree and taken back out, it was in the branch's reflog,
# which let submit take it for an amend of dockhand's own and replace it
# (the rc4 full stage, herbyg-test/macports-ports#84).
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
	local fork outside=$ROW_DIR/outside commit
	fork=$(git -C "$dir" remote get-url --push origin) || return 1
	rm -rf "$outside"
	# One commit deep and with no files checked out: the commit is made
	# from its parent's tree, an empty commit, as the worktree's was.
	git clone -q --depth 1 --no-checkout --branch "dockhand/$DS1_BRANCH" "$fork" "$outside" >>"$ROW_DIR/out.log" 2>&1 || return 1
	commit=$(GIT_AUTHOR_NAME=Someone GIT_AUTHOR_EMAIL=someone@example.invalid GIT_COMMITTER_NAME=Someone GIT_COMMITTER_EMAIL=someone@example.invalid \
		git -C "$outside" commit-tree "HEAD^{tree}" -p HEAD -m "someone else's commit") || return 1
	git -C "$outside" push -q origin "$commit:refs/heads/dockhand/$DS1_BRANCH" >>"$ROW_DIR/out.log" 2>&1 || return 1
	rm -rf "$outside"
	# A commit of dockhand's own on top, so there's something to push.
	printf '# a later edit\n' >>"$dir/$(dh_quiet --json status "$DS1_BRANCH" | jq -r '.result.branches[0].directories[0]')/Portfile"
	dh tidy -b "$DS1_BRANCH" -y || return 1
}
act() {
	host_only "a submitted branch on the fork" || return 0
	[ -n "${DS1_BRANCH:-}" ] || return 0
	# --no-check, as its first submit: setup makes no check, and a refusal
	# for want of one would pass the row for the wrong reason.
	dh submit -b "$DS1_BRANCH" --plan --no-check || :
	dh_json submit -b "$DS1_BRANCH" -y --no-check || :
	close_test_pr "$(port)" "$DS1_BRANCH"
}
assert() {
	local submit
	submit=$(grep -l '^submit .* -y' "$ROW_DIR"/json/*.json.args 2>/dev/null | tail -1)
	[ "$(cat "${submit%.args}.exit" 2>/dev/null)" != 0 ] || { row_fail "submit went ahead over someone's push"; return; }
	grep -q "Push .*won't push: someone else pushed" "$ROW_DIR/out.log" ||
		{ row_fail "the preview's Push line didn't say it won't push"; return; }
	jq -r '.error // empty' "${submit%.args}" | matches -i "no check has finished" &&
		{ row_fail "submit refused for want of a check, not for someone's push"; return; }
	row_pass "refused: $(jq -r '.error // empty' "${submit%.args}")"
}
