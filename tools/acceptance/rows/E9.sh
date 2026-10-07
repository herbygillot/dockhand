# stages: quick full
# E9: adopt --pr and review are experimental until the trust rule is in
# code: review of a public pull request prints the notice, saying whose
# Portfile it evaluates. ACCEPT_REVIEW_PR names it.
act() {
	local pr=${ACCEPT_REVIEW_PR:-34756}
	# The quick stage's pinned upstream has no pull requests' refs; review
	# fetches the pull request's head from it, so the row gives it this
	# one's, read from MacPorts' repository (the M1's run at 10aac0c3).
	if [ "${ACCEPT_STAGE:-}" = quick ] && [ -n "${DOCKHAND_UPSTREAM:-}" ]; then
		git -C "$DOCKHAND_UPSTREAM" fetch -q https://github.com/macports/macports-ports.git "refs/pull/$pr/head:refs/pull/$pr/head" >>"$ROW_DIR/out.log" 2>&1 || :
	fi
	# The pull request is MacPorts', so review reads MacPorts': the stage's
	# DOCKHAND_PULL_REQUESTS names its sandbox, where #34756 isn't (the rc6
	# full stage).
	(unset DOCKHAND_PULL_REQUESTS; dh review "$pr" --markdown </dev/null) || :
}
assert() {
	if grep -qi 'experimental' "$ROW_DIR/out.log"; then
		row_pass "review says it's experimental: $(grep -i experimental "$ROW_DIR/out.log" | head -1)"
	else
		row_fail "review printed no experimental notice"
	fi
}
