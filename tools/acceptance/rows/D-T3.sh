# stages: quick full
# D-T3: a port that doesn't exist, a misspelled flag, and --tests
# misspelled: each refused before anything is captured or fetched.
act() {
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
	elif [ -n "$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*')" ]; then
		row_fail "a branch was started"
	elif grep -q 'Fetching' "$ROW_DIR/out.log"; then
		row_fail "something was fetched before the refusal"
	else
		row_pass "each refused before anything was captured: $(jq -r .error "$ROW_DIR/json/1.json"); $(jq -r .error "$ROW_DIR/json/3.json")"
	fi
}
