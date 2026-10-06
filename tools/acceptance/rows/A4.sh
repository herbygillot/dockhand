# stages: full
# A4: the README's "An update, start to finish" followed word for word,
# on a port that's due. Where the README and the tool disagree, one is
# fixed. A10 runs the same examples as tests; this is a person's reading.
# prs: ${ACCEPT_GO_PORT} test
act() {
	host_only "a person following the README on the host" || return 0
	allow_change "*"
	# The person's submit pushes the branch to the fork and opens the one
	# test pull request; H2 called that push harm (the rc3 full run).
	allow_push "*dockhand/$ACCEPT_GO_PORT*"
	allow_prs 1
	# A dhtest Terminal hasn't the harness's environment, so a plain
	# submit there went to MacPorts (the rc1 full stage): the row writes
	# what it needs, which the person sources first, and says how submit
	# makes the pull request a test one, as submit_pr does.
	cat >"$ROW_DIR/env.sh" <<ENV
export DOCKHAND_PULL_REQUESTS=$DOCKHAND_PULL_REQUESTS
export GIT_SSH_COMMAND="$GIT_SSH_COMMAND"
export MACPORTS_TREE=$MACPORTS_TREE
export PATH="/opt/local/bin:/opt/local/sbin:\$PATH"
ENV
	local image=""
	"$DH_BIN" providers 2>/dev/null | grep -q '✓ images for' || image=" Its check builds in Tart's plain image, which this home hasn't: make it first with dockhand setup tart, the README's setup."
	checkpoint "in a dhtest Terminal, source $ROW_DIR/env.sh, then follow the README's \"An update, start to finish\" word for word on $ACCEPT_GO_PORT. At submit, add --title \"[testing] <the commit's subject>\" --skip-notification --note \"This pull request tests a dockhand release candidate, ${ACCEPT_CANDIDATE:-}, and will be closed without merging.\", so it's a test pull request in $DOCKHAND_PULL_REQUESTS.$image Note every disagreement with the README in $ROW_DIR/notes" || return 0
}
assert() {
	if [ -s "$ROW_DIR/notes" ]; then
		row_fail "the README and the tool disagree: $(tr '\n' ';' <"$ROW_DIR/notes")"
	else
		row_pass "the README worked as written"
	fi
}
