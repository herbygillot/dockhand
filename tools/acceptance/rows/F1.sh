# stages: full
# F1: a GitHub account with no MacPorts write access, the test account:
# submit works from the fork; --ready takes the gh fallback or says how;
# a first-time contributor's CI, waiting for a maintainer's approval,
# reads as waiting. That last can't happen in the sandbox, whose owner
# the test account is, so it's the Go suite's
# (TestCIAwaitingApprovalReadsAsWaiting), not this row's.
# prs: ${ACCEPT_GO_PORT} test
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "the test account on GitHub" || return 0
	dh_setup update "$(port)" --new || return 0
	F1_BRANCH=$(own_branch)
	dh check -b "$F1_BRANCH" && dh tidy -b "$F1_BRANCH" -y || return 0
	submit_pr "$(port)" "$F1_BRANCH" --draft || return 0
	# -y, since without a terminal submit goes ahead only with it (the rc6
	# full stage).
	dh_json submit -b "$F1_BRANCH" --ready -y || :
	dh_json status --refresh "$F1_BRANCH" || :
	close_test_pr "$(port)" "$F1_BRANCH"
}
assert() {
	judged "submit worked from the fork, and --ready took the gh fallback or said how (CI awaiting approval is the Go suite's)"
}
