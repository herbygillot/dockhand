# stages: quick full
# E9: adopt --pr and review are experimental until the trust rule is in
# code: review of a public pull request prints the notice, saying whose
# Portfile it evaluates. ACCEPT_REVIEW_PR names it.
act() {
	dh review "${ACCEPT_REVIEW_PR:-34756}" --markdown </dev/null || :
}
assert() {
	if grep -qi 'experimental' "$ROW_DIR/out.log"; then
		row_pass "review says it's experimental: $(grep -i experimental "$ROW_DIR/out.log" | head -1)"
	else
		row_fail "review printed no experimental notice"
	fi
}
