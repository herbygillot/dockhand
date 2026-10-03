# stages: full
# D-S1: someone pushes to the fork branch after submit: the next submit
# refuses rather than overwriting (H2).
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	host_only "a submitted branch on the fork" || return 0
	checkpoint "push a commit to a submitted test branch on the fork from elsewhere, naming the branch in $ROW_DIR/branch" || return 0
	dh_json submit -b "$(cat "$ROW_DIR/branch")" -y || :
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit")" != 0 ] && row_pass "refused: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")" ||
		row_fail "submit went ahead over someone's push"
}
