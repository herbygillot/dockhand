# stages: quick full
# B2: one shot, with no serve running: bump on the small Rust port, asking
# nothing, to its pull request, or exiting 3 where it's held, naming the
# command that finishes it. bump settles what would stop its submit before
# it edits anything, so in the quick stage, which has no GitHub fork, it
# refuses at once, naming the fork: the full stage runs it through.
act() {
	dh_json bump "${ACCEPT_RUST_PORT:?}" </dev/null || :
}

assert() {
	local file=$ROW_DIR/json/1.json status error
	status=$(cat "$file.exit")
	error=$(jq -r '.error // ""' "$file")
	if grep -qE '\[y/N\]|\[Y/n\]' "$ROW_DIR/out.log"; then
		row_fail "bump asked something"
		return
	fi
	case "$status" in
	0) row_pass "bumped to its pull request" ;;
	3) if grep -q '^Once it.s fine: dockhand submit' "$ROW_DIR/out.log" || printf '%s' "$error" | grep -q 'dockhand submit'; then
		row_pass "held for a look, naming the submit that finishes it"
	else
		row_fail "held without naming the command that finishes it: $error"
	fi ;;
	*) if printf '%s' "$error" | grep -qi fork && [ -z "$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*')" ]; then
		row_refused_well "refused before editing anything, for want of a GitHub fork, which the quick stage hasn't: $error"
	else
		row_fail "bump exited $status: $error"
	fi ;;
	esac
}
