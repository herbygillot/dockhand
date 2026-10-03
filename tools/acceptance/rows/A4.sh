# stages: full
# A4: the README's "An update, start to finish" followed word for word,
# on a port that's due. Where the README and the tool disagree, one is
# fixed. A10 runs the same examples as tests; this is a person's reading.
# prs: ${ACCEPT_GO_PORT} test
act() {
	host_only "a person following the README on the host" || return 0
	allow_change "*"
	checkpoint "follow the README's \"An update, start to finish\" word for word on $ACCEPT_GO_PORT, as a test PR; note every disagreement in $ROW_DIR/notes" || return 0
}
assert() {
	if [ -s "$ROW_DIR/notes" ]; then
		row_fail "the README and the tool disagree: $(tr '\n' ';' <"$ROW_DIR/notes")"
	else
		row_pass "the README worked as written"
	fi
}
