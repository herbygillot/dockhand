# stages: quick full
# G31: MacPorts' builders' history in impact (868a208c). On a branch
# editing gitui, impact adds a Builders row from ports.macports.org's
# builds, each builder's latest, "gitui: failed on … ; built on the other
# N, by each one's latest build on ports.macports.org", and its JSON a
# builders list with each builder's latest build. Over the network; a
# site that doesn't answer says nothing and fails nothing, so where impact
# has no builders the row isn't run, saying so. impact's text names a
# check of a dependent, which the quick stage run without an image can't
# plan, so it's read as JSON.
setup() {
	G31_BRANCH=$(run_name g31)
	dh_setup start "$G31_BRANCH" --port gitui </dev/null || return 1
}
act() {
	local dir
	dir=$("$DH_BIN" path "$G31_BRANCH") || return 0
	allow_change "$dir/*"
	printf '\n# an edit of the row'"'"'s\n' >>"$dir/devel/gitui/Portfile"
	dh_json impact -b "$G31_BRANCH" </dev/null || :
}
assert() {
	local json=$ROW_DIR/json/1.json
	[ -f "$json" ] || { row_fail "impact didn't run"; return; }
	if [ "$(cat "$json.exit")" != 0 ]; then
		row_fail "impact exited $(cat "$json.exit"): $(jq -r '.error // empty' "$json")"
		return
	fi
	if [ "$(jq '[.result.builders[]? | select(.port == "gitui")] | length' "$json")" = 0 ]; then
		row_result "not run" "impact said no builders for gitui; ports.macports.org may not have answered within 5 s"
		return
	fi
	if [ "$(jq '[.result.builders[] | select(.port == "gitui") | .latest[]?] | length' "$json")" = 0 ]; then
		row_fail "impact's builders for gitui name no builder's latest build: $(jq -c '.result.builders' "$json" | cut -c1-300)"
		return
	fi
	if ! jq -r '.result.builders[] | select(.port == "gitui") | .words' "$json" | matches -E '(built|failed) on macOS'; then
		row_fail "impact's builders words for gitui don't say a release: $(jq -r '.result.builders[] | select(.port == "gitui") | .words' "$json")"
		return
	fi
	row_pass "impact's Builders: gitui: $(jq -r '.result.builders[] | select(.port == "gitui") | .words' "$json" | cut -c1-200), from $(jq '[.result.builders[] | select(.port == "gitui") | .latest[]] | length' "$json") builders' latest builds"
}
