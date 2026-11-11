# stages: quick full
# E9: review of a public pull request in a script evaluates nothing of its
# author's without --evaluate: a text-only review whose first line says
# so, "Text-only review: #34756's Portfiles were not evaluated", and no
# "Evaluating" notice; with --evaluate it says, once, whose Portfiles it
# evaluates on this Mac, "Evaluating @cdeil's Portfiles on this Mac, for
# #34756" (#162). ACCEPT_REVIEW_PR names it; G22's sandbox pull request is
# ACCEPT_SANDBOX_PR, since setting one for both had E9 review the
# sandbox's number in MacPorts' (rc1's quick stage).
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
	(unset DOCKHAND_PULL_REQUESTS; dh review "$pr" --markdown --evaluate </dev/null) || :
}
assert() {
	local text said
	text=$(sed -n '1,/^\[exit/p' "$ROW_DIR/out.log")
	if ! printf '%s\n' "$text" | matches -E "^Text-only review: #[0-9]+'s Portfiles were not evaluated"; then
		row_fail "a script's review without --evaluate wasn't text-only"
		return
	fi
	if printf '%s\n' "$text" | matches -E "Evaluating @[^ ]+ Portfiles on this Mac"; then
		row_fail "a text-only review evaluated the author's Portfiles"
		return
	fi
	said=$(grep -E -m1 "Evaluating @[^ ]+ Portfiles on this Mac" "$ROW_DIR/out.log")
	if [ -n "$said" ]; then
		row_pass "a script's review is text-only, and with --evaluate says whose Portfiles it evaluates: $said"
	else
		row_fail "review --evaluate didn't say whose Portfiles it evaluates on this Mac"
	fi
}
