# stages: full
# C7: clean over merged, closed, and archived branches, one with edits,
# and one whose fork branch moved after merge: each kept or removed as
# usage.md says, H1 and H2 holding.
#
# The row makes those branches in setup, before the harm sweep's
# snapshot, as B6 makes its merge: two test pull requests merged in the
# sandbox, the second's fork branch moved by a commit after its merge, a
# third closed with an uncommitted edit left in its worktree, and a branch
# archived. A checkpoint had a person make them, inside the snapshot's
# window, and the run had none of them by C7 (the rc6 full stage). Its
# teardown sets the sandbox's master back to MacPorts'.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

# c7_pr makes a branch of the port's update, tidied, with a test pull
# request in the sandbox, submitted without a check, since only its state
# matters here; it says the branch.
c7_pr() {
	local branch
	dh update "$(port)" --new >/dev/null || return 1
	branch=$(own_branch_new)
	dh tidy -b "$branch" -y >/dev/null || return 1
	submit_pr "$(port)" "$branch" --no-check >/dev/null || return 1
	printf '%s' "$branch"
}

# own_branch_new is the open branch made since the last call, which
# own_branch can't tell apart once the row has made several.
own_branch_new() {
	local branch
	branch=$("$DH_BIN" --json status 2>/dev/null | jq -r '.result.branches[]?.name' | grep -vxF -f "$ROW_DIR/branches.seen" 2>/dev/null | tail -1)
	printf '%s\n' "$branch" >>"$ROW_DIR/branches.seen"
	printf '%s' "$branch"
}

c7_url() { dh_quiet --json status "$1" | jq -r '.result.branches[0].pull_request.url // empty'; }

setup() {
	host_only "merged and closed pull requests" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull requests for $(port) aren't approved test ones, and it merges and closes what it opens"
		return 0
	fi
	cp "$ROW_DIR/branches.before" "$ROW_DIR/branches.seen"
	local merged moved closed archived worktree
	merged=$(c7_pr) || return 1
	gh pr merge "$(c7_url "$merged")" --merge >>"$ROW_DIR/out.log" 2>&1 || return 1
	moved=$(c7_pr) || return 1
	gh pr merge "$(c7_url "$moved")" --merge >>"$ROW_DIR/out.log" 2>&1 || return 1
	# The fork branch moves after its merge: a commit pushed to it.
	worktree=$(dh_quiet path "$moved")
	git -C "$worktree" commit -q --allow-empty -m "after the merge" || return 1
	git -C "$worktree" push -q origin "HEAD:refs/heads/dockhand/$moved" >>"$ROW_DIR/out.log" 2>&1 || return 1
	closed=$(c7_pr) || return 1
	gh pr close "$(c7_url "$closed")" >>"$ROW_DIR/out.log" 2>&1 || return 1
	printf '# an edit no commit has\n' >>"$(dh_quiet path "$closed")/$(dh_quiet --json status "$closed" | jq -r '.result.branches[0].directories[0]')/Portfile"
	# A name of this run's own: an archived branch keeps its name.
	archived=c7-archived-$(date +%s)
	dh start "$archived" >/dev/null || return 1
	printf '%s\n' "$archived" >>"$ROW_DIR/branches.seen"
	dh archive "$archived" >/dev/null || return 1
	printf 'merged %s\nmoved %s\nclosed %s\narchived %s\n' "$merged" "$moved" "$closed" "$archived" >"$ROW_DIR/fixtures"
	c7_reset_sandbox
}

# c7_reset_sandbox sets the sandbox's master back to MacPorts'.
c7_reset_sandbox() {
	local upstream
	upstream=$(gh api repos/macports/macports-ports/git/ref/heads/master --jq .object.sha 2>>"$ROW_DIR/out.log") || return 0
	gh api -X PATCH "repos/${DOCKHAND_PULL_REQUESTS:?}/git/refs/heads/master" -f sha="$upstream" -F force=true --silent >>"$ROW_DIR/out.log" 2>&1 || :
}

teardown() {
	[ -f "$ROW_DIR/fixtures" ] && c7_reset_sandbox
	return 0
}

act() {
	host_only "merged and closed pull requests" || return 0
	[ -f "$ROW_DIR/fixtures" ] || return 0
	dh status --refresh || :
	dh_json clean --closed || :
	allow_change "*"
	allow_ref_gone "*"
	dh_json clean --closed -y || :
	dh status --all || :
}
assert() {
	local clean
	clean=$(grep -l '^clean --closed -y$' "$ROW_DIR"/json/*.json.args 2>/dev/null | head -1)
	[ "$(cat "${clean%.args}.exit" 2>/dev/null)" = 0 ] || { row_fail "clean failed"; return; }
	judged "from $ROW_DIR/fixtures: clean kept the branch with edits and the moved fork branch, and removed the rest as usage.md says"
}
