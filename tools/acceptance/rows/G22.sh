# stages: full
# G22: review --check after the author pushes over an adopted pull request
# (Codex's workflow review of 2ad896d2, W2). review <N> --check adopts and
# checks ACCEPT_SANDBOX_PR, an open pull request in the sandbox that this
# database didn't open and that a person can push to as its author; after
# their push, review --check moves the adopted branch to the new head and
# checks that; after a local commit on the branch and another push, it's
# refused, naming the commit beyond the head it was adopted at and dockhand
# check -b, which checks the branch as it is. Without the pull request, the
# row isn't run.
pr() { printf '%s' "${ACCEPT_SANDBOX_PR:-}"; }

act() {
	host_only "a sandbox pull request its author pushes to" || return 0
	if [ -z "$(pr)" ]; then
		row_result "not run" "set ACCEPT_SANDBOX_PR to an open pull request in the sandbox this database didn't open, which you can push to as its author"
		return 0
	fi
	allow_change "*"
	# The author's pushes to its branch are the row's, which H2 counted
	# (rc1's full stage).
	local head
	head=$(gh pr view "$(pr)" --repo "${DOCKHAND_PULL_REQUESTS:?}" --json headRefName --jq .headRefName 2>/dev/null)
	[ -z "$head" ] || allow_push "*$head"
	dh review "$(pr)" --check </dev/null || :
	checkpoint "push a new commit to #$(pr)'s branch, as its author" || return 0
	G22_HEAD=$(gh pr view "$(pr)" --repo "${DOCKHAND_PULL_REQUESTS:?}" --json headRefOid --jq .headRefOid 2>/dev/null)
	dh review "$(pr)" --check </dev/null || :
	local worktree
	worktree=$("$DH_BIN" path "pr-$(pr)" 2>/dev/null) || :
	[ -n "$worktree" ] && git -C "$worktree" -c user.name=acceptance -c user.email=acceptance@example.invalid -c commit.gpgsign=false \
		commit -q --allow-empty -m "a local commit beyond the pull request's head" || :
	checkpoint "push another commit to #$(pr)'s branch, as its author" || return 0
	dh review "$(pr)" --check </dev/null || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G22_HEAD:-}" ] || return 0
	local second third
	second=$(awk '/^\$ dockhand review/{n++} n==2' "$ROW_DIR/out.log")
	third=$(awk '/^\$ dockhand review/{n++} n==3' "$ROW_DIR/out.log")
	printf '%s\n' "$second" | matches -F "[exit 0]" || { row_fail "review --check after the author's push failed: $(printf '%s\n' "$second" | tail -3 | tr '\n' ' ')"; return; }
	printf '%s\n' "$second" | matches -F "${G22_HEAD:0:7}" || { row_fail "review --check after the author's push didn't check its new head ${G22_HEAD:0:7}"; return; }
	printf '%s\n' "$third" | matches -E "beyond #$(pr)'s head .*dockhand check -b " ||
		{ row_fail "review --check with a local commit beyond the head wasn't refused naming it: $(printf '%s\n' "$third" | tail -3 | tr '\n' ' ')"; return; }
	row_pass "review --check checked #$(pr)'s new head after its author's push, and refused once the adopted branch had a commit beyond it, naming check -b"
}
