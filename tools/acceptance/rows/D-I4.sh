# stages: full
# D-I4: submit killed with SIGKILL at its two dangerous steps, after the
# push and before GitHub opens the pull request, and after GitHub has it
# and before dockhand records it. Submitting again opens exactly one pull
# request, or finds the one GitHub has, never a second (H3).
#
# The kill lands at the step itself, by the candidate's own source built
# with the acceptance failpoints (failpoint_bin): the release binary,
# killed when its -v output named the step, said no such step, and the
# kill never came (the rc6 full stage). Each kill is a branch of its own;
# the submits are --no-check, since what's tested is the pull request.
# prs: ${ACCEPT_RUST_PORT} test
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }

# di4_branch makes a tidied branch of the port's update, and says it.
di4_branch() {
	"$DH_BIN" update "$(port)" --new >>"$ROW_DIR/out.log" 2>&1 || return 1
	local branch
	branch=$("$DH_BIN" --json status 2>/dev/null | jq -r '.result.branches[]?.name' | grep -vxF -f "$ROW_DIR/branches.seen" | tail -1)
	printf '%s\n' "$branch" >>"$ROW_DIR/branches.seen"
	"$DH_BIN" tidy -b "$branch" -y >>"$ROW_DIR/out.log" 2>&1 || return 1
	printf '%s' "$branch"
}

# di4_count is how many pull requests the sandbox has from a branch.
di4_count() {
	gh pr list --repo "${DOCKHAND_PULL_REQUESTS:?}" --head "dockhand/$1" --state all --json number --jq length 2>/dev/null
}

setup() {
	host_only "a pull request on GitHub" || return 0
	if [ "$(pr_kind "$(port)")" != test ]; then
		row_result "not run" "its pull requests for $(port) aren't approved test ones, and it closes what it opens"
		return 0
	fi
	DI4_FP=$(failpoint_bin) || { row_fail "the candidate couldn't be built with failpoints"; return 1; }
	cp "$ROW_DIR/branches.before" "$ROW_DIR/branches.seen"
}

act() {
	host_only "a pull request on GitHub" || return 0
	[ -n "${DI4_FP:-}" ] || return 0
	local step branch words=(--no-check --title "[testing] $(port): a dockhand kill test" --skip-notification
		--note "This pull request tests a dockhand release candidate, ${ACCEPT_CANDIDATE:-}, and will be closed without merging.")
	allow_prs 2
	for step in pushed created; do
		branch=$(di4_branch) || { row_fail "its branch for the $step kill couldn't be made"; return 0; }
		printf '%s %s\n' "$step" "$branch" >>"$ROW_DIR/kills"
		allow_push "*$branch*"
		printf '$ DOCKHAND_FAILPOINT=submit.%s:kill dockhand submit -b %s -y\n' "$step" "$branch" >>"$ROW_DIR/out.log"
		DOCKHAND_FAILPOINT="submit.$step:kill" "$DI4_FP" submit -b "$branch" -y "${words[@]}" </dev/null >>"$ROW_DIR/out.log" 2>&1
		printf '[exit %d] killed at submit.%s\n' "$?" "$step" >>"$ROW_DIR/out.log"
		dh_json submit -b "$branch" -y "${words[@]}" || :
		printf '%s\n' "$(di4_count "$branch")" >"$ROW_DIR/count.$step"
		close_test_pr "$(port)" "$branch"
	done
}

assert() {
	local step
	for step in pushed created; do
		grep -q "^\[exit 137\] killed at submit.$step$" "$ROW_DIR/out.log" ||
			{ row_fail "submit wasn't killed at submit.$step"; return; }
		[ "$(cat "$ROW_DIR/count.$step" 2>/dev/null)" = 1 ] ||
			{ row_fail "after the kill at submit.$step, the sandbox has $(cat "$ROW_DIR/count.$step" 2>/dev/null) pull requests from its branch, not one"; return; }
	done
	row_pass "killed after the push and after GitHub opened the pull request, submit again left exactly one pull request each time"
}
