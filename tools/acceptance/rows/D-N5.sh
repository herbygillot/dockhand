# stages: full
# D-N5: upstream retags a release between update and check. A Git-fetched
# port is out of scope, so a branch whose tag moved is planned with
# --plan: check --plan says the target builds again, its tag naming
# another commit, and submit's preview holds on it.
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
	# In prose, where the plan's notes are said.
	dh check -b "$ACCEPT_DN5_BRANCH" --plan || :
	dh submit -b "$ACCEPT_DN5_BRANCH" --plan || :
}
assert() {
	# Both say it: check --plan, from its plans' tags, and submit's
	# preview, as a hold, which rc1's preview stopped short of on a
	# refused key (rc1's full stage).
	local plan preview
	# Each joined, as dockhand wraps its lines off a terminal.
	plan=$(sed -n '/^\$ dockhand check /,/^\[exit/p' "$ROW_DIR/out.log" | tr '\n' ' ' | tr -s ' ')
	preview=$(sed -n '/^\$ dockhand submit /,/^\[exit/p' "$ROW_DIR/out.log" | tr '\n' ' ' | tr -s ' ')
	printf '%s' "$plan" | matches -E 'builds again: .* now names .*, not ' ||
		{ row_fail "check --plan didn't say the tag moved"; return; }
	printf '%s' "$preview" | matches -E 'now names .*, not .*, as when' ||
		{ row_fail "submit's preview didn't hold on the moved tag: $(printf '%s' "$preview" | tail -c 300)"; return; }
	row_pass "check --plan and submit's preview said the tag moved"
}
