# stages: quick full
# G25: a newer tag under another prefix is said (651799bc, the
# outdated-ports session's note 17). alot's github.setup names no tag
# prefix, its tags moved to v0.11 and v0.12, and outdated offered 0.11,
# the newest tag without a v; outdated alot says "upstream's newest tag,
# v0.12, doesn't match github.setup's tag prefix" beside the update, and
# --json carries other_prefix. Over the network, at the stage's pinned
# master; where alot has a tag past 0.11 without a v, the row isn't run.
act() {
	dh outdated alot </dev/null || :
	dh_json outdated alot </dev/null || :
}
assert() {
	local json=$ROW_DIR/json/1.json
	[ -f "$json" ] || { row_fail "outdated alot didn't run"; return; }
	if [ "$(jq -r '.result.ports[0].newest // empty' "$json")" != 0.11 ]; then
		row_result "not run" "outdated alot offers $(jq -r '.result.ports[0].newest // "nothing"' "$json") now, not 0.11 beside v0.12"
		return
	fi
	if [ -z "$(jq -r '.result.ports[0].other_prefix // empty' "$json")" ]; then
		row_fail "outdated --json alot, offering 0.11, carried no other_prefix for v0.12: $(jq -c '.result.ports[0]' "$json")"
		return
	fi
	if ! grep -A3 -F '$ dockhand outdated alot' "$ROW_DIR/out.log" | tr '\n' ' ' | sed 's/  */ /g' | matches "doesn't match github.setup's tag prefix"; then
		row_fail "outdated alot didn't say the newer tag under another prefix: $(grep -A3 -F '$ dockhand outdated alot' "$ROW_DIR/out.log" | sed -n 2,4p | tr '\n' ' ')"
		return
	fi
	row_pass "outdated alot offers 0.11 and says $(jq -r '.result.ports[0].other_prefix.words // .result.ports[0].other_prefix' "$json")"
}
