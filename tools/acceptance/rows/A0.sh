# stages: quick full
# A0: make test on the candidate, on the Mac, as a person builds it, with
# none of the stage's environment.
act() {
	local tclsh=/opt/local/libexec/macports/bin/tclsh8.6 status=0
	(
		cd "$ACCEPT_REPO" || exit 1
		# Nothing of the stage's reaches the tests: its Tart homes, its
		# token, which a test reaching GitHub would spend, its SSH, and
		# its sandbox (the M1's run at 10aac0c3).
		unset DOCKHAND_DB DOCKHAND_CONFIG MACPORTS_TREE DOCKHAND_UPSTREAM DOCKHAND_INDEX_CACHE DOCKHAND_READING_CACHE DOCKHAND_INDEX_MIRROR \
			DOCKHAND_TART_HOME DOCKHAND_SSH_DIR TART_HOME GH_TOKEN GITHUB_TOKEN GIT_SSH_COMMAND DOCKHAND_PULL_REQUESTS HTTPS_PROXY HTTP_PROXY
		[ -x "$tclsh" ] && export DOCKHAND_TEST_MACPORTS_TCLSH=$tclsh
		make test
	) >"$ROW_DIR/make-test.log" 2>&1 || status=$?
	echo "$status" >"$ROW_DIR/a0.exit"
}
assert() {
	if [ "$(cat "$ROW_DIR/a0.exit")" = 0 ]; then
		row_pass "make test passed"
	else
		row_fail "make test failed: $(grep -E '^(--- FAIL|FAIL)' "$ROW_DIR/make-test.log" | head -3 | tr '\n' ';')"
	fi
}
