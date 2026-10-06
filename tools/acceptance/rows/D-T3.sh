# stages: quick full
# D-T3: a port that doesn't exist, a misspelled flag, and --tests
# misspelled: each refused before anything is captured or fetched.
act() {
	# The branches there before the row, which earlier rows left: only one
	# the row starts is a failure (the rc6 full stage).
	git -C "$MACPORTS_TREE" branch --list 'dockhand/*' >"$ROW_DIR/git-branches.before"
	dh_json update no-such-port-dhaccept --new || :
	dh_json update "${ACCEPT_GO_PORT:?}" --nwe || :
	dh_json check --tests al </dev/null || :
}
assert() {
	local n bad=""
	for n in 1 2 3; do
		[ "$(cat "$ROW_DIR/json/$n.json.exit")" = 0 ] && bad="$bad $(cat "$ROW_DIR/json/$n.json.args")"
	done
	if [ -n "$bad" ]; then
		row_fail "not refused:$bad"
	elif [ -n "$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*' | grep -vxF -f "$ROW_DIR/git-branches.before")" ]; then
		row_fail "a branch was started"
	elif grep -q 'Fetching' "$ROW_DIR/out.log"; then
		row_fail "something was fetched before the refusal"
	else
		row_pass "each refused before anything was captured: $(jq -r .error "$ROW_DIR/json/1.json"); $(jq -r .error "$ROW_DIR/json/3.json")"
	fi
}
