# stages: quick full
# G30: PortGroup change made visible (f38a0d04). A branch that changes
# _resources/port1.0/group/notabug-1.0.tcl's default homepage, which moves
# cl-clunit2's evaluation, the one port at the stage's pinned master that
# loads it, beside an edit of cl-clunit2's own: once tidy --plan has made
# the branch's change records, status puts _resources first, saying
# "_resources/port1.0/group/notabug-1.0.tcl changed, moving cl-clunit2",
# where it named _resources alone.
setup() {
	G30_BRANCH=$(run_name g30)
	dh_setup start "$G30_BRANCH" --port cl-clunit2 </dev/null || return 1
}
act() {
	local dir
	dir=$("$DH_BIN" path "$G30_BRANCH") || return 0
	allow_change "$dir/*"
	perl -pi -e 's|https://notabug\.org/|https://codeberg.org/|' "$dir/_resources/port1.0/group/notabug-1.0.tcl"
	printf '\n# an edit of the row'"'"'s\n' >>"$dir/lisp/cl-clunit2/Portfile"
	dh_json tidy -b "$G30_BRANCH" --plan </dev/null || :
	dh status "$G30_BRANCH" </dev/null || :
}
assert() {
	local line
	line=$(sed -n "/^\\\$ dockhand status $G30_BRANCH\$/,/^\\[exit/p" "$ROW_DIR/out.log" | grep -E '^ *Ports ' | head -1)
	[ -n "$line" ] || { row_fail "status $G30_BRANCH printed no Ports line: $(sed -n "/^\\\$ dockhand status $G30_BRANCH\$/,/^\\[exit/p" "$ROW_DIR/out.log" | sed 1d | head -4 | tr '\n' ' ')"; return; }
	if ! printf '%s\n' "$line" | matches -F '_resources (_resources/port1.0/group/notabug-1.0.tcl changed, moving cl-clunit2)'; then
		row_fail "status's Ports line didn't say the PortGroup's change and the port it moved: $line"
		return
	fi
	row_pass "status: $(printf '%s' "$line" | sed 's/^ *//')"
}
