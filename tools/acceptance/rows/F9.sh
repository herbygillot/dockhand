# stages: quick full
# F9: terminals: TERM=dumb, NO_COLOR, 60 columns, and output piped to a
# file. What dockhand writes is readable, with no escape codes where none
# belong.
act() {
	(
		export TERM=dumb NO_COLOR=1 COLUMNS=60
		dh status || :
		dh --help || :
		dh config || :
		dh outdated "${ACCEPT_GO_PORT:?}" || :
	)
}
assert() {
	if LC_ALL=C grep -q "$(printf '\033')" "$ROW_DIR/out.log"; then
		row_fail "escape codes in output piped to a file: $(LC_ALL=C grep -n "$(printf '\033')" "$ROW_DIR/out.log" | head -2 | cat -v | tr '\n' ';')"
	else
		row_pass "no escape codes under TERM=dumb, NO_COLOR, 60 columns, piped"
	fi
}
