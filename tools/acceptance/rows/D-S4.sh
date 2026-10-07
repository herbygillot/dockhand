# stages: quick full
# D-S4: a hand commit, and a hand edit to a file dockhand edited, in the
# worktree: tidy keeps both, and proposes them as yours.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	local dir portfile
	# The row's own branch: the first dockhand/* ref, alphabetically, was
	# b10-broken, archived, and the hand commit went on it (the rc6 full
	# stage).
	dir=$("$DH_BIN" path "$(own_branch)") || return 0
	portfile=$(find "$dir" -path "*/$(port)/Portfile" | head -1)
	allow_change "$portfile"
	printf 'notes\n' >"$dir/HAND.txt"
	git -C "$dir" add HAND.txt
	git -C "$dir" -c user.name=Ada -c user.email=ada@example.org commit -q -m "a hand commit" -- HAND.txt
	printf '# a hand edit\n' >>"$portfile"
	DS4_DIR=$dir DS4_PORTFILE=$portfile
	dh tidy -b "$(basename "$dir")" --plan </dev/null || :
}
assert() {
	local plan
	plan=$(sed -n '/^\$ dockhand tidy/,/^\[exit/p' "$ROW_DIR/out.log")
	if ! printf '%s' "$plan" | grep -qi 'did not make\|review them\|yours'; then
		row_fail "tidy's plan didn't say the hand edits are yours: $(printf '%s' "$plan" | head -5 | tr '\n' ';')"
	elif ! grep -q '# a hand edit' "$DS4_PORTFILE" || [ ! -f "$DS4_DIR/HAND.txt" ]; then
		row_fail "a hand edit is gone"
	else
		row_pass "tidy's plan keeps the hand commit and edit, as yours"
	fi
}
