# stages: quick full
# G29: a branch of updates of several ports to one version is titled from
# their subjects (a2359ca6, field testing's smaller asks): two commits,
# "alot: update to 0.28.2" and "gitui: update to 0.28.2", on one branch;
# submit --plan's title is "alot, gitui: update to 0.28.2", where it said
# "Title (none)" and blocked for --title. The versions are the row's, not
# releases: the title is read from the subjects.
setup() {
	G29_BRANCH=$(run_name g29)
	dh_setup start "$G29_BRANCH" --port alot --port gitui </dev/null || return 1
}
act() {
	local dir
	dir=$("$DH_BIN" path "$G29_BRANCH") || return 0
	allow_change "$dir/*"
	perl -pi -e 's/^(github\.setup\s+pazz alot) 0\.3\.4/$1 0.28.2/' "$dir/mail/alot/Portfile"
	git -C "$dir" commit -q -am "alot: update to 0.28.2" >>"$ROW_DIR/out.log" 2>&1 || return 0
	perl -pi -e 's/^(github\.setup\s+extrawurst gitui) 0\.28\.1 v/$1 0.28.2 v/' "$dir/devel/gitui/Portfile"
	git -C "$dir" commit -q -am "gitui: update to 0.28.2" >>"$ROW_DIR/out.log" 2>&1 || return 0
	dh submit -b "$G29_BRANCH" --plan </dev/null || :
	dh_json submit -b "$G29_BRANCH" --plan </dev/null || :
}
assert() {
	local json=$ROW_DIR/json/1.json
	[ -f "$json" ] || { row_fail "submit --plan didn't run"; return; }
	if [ "$(jq -r '.result.title // empty' "$json")" != "alot, gitui: update to 0.28.2" ]; then
		row_fail "submit --plan's title is $(jq -r '.result.title // "none"' "$json"), not alot, gitui: update to 0.28.2"
		return
	fi
	if ! grep -F '  Title ' "$ROW_DIR/out.log" | matches -F 'alot, gitui: update to 0.28.2'; then
		row_fail "submit --plan's text didn't title it: $(grep -F '  Title ' "$ROW_DIR/out.log" | head -1)"
		return
	fi
	if jq -r '.result.blocking[]? | tostring' "$json" | matches -i 'title'; then
		row_fail "submit --plan still blocks for a title: $(jq -r '.result.blocking[]? | tostring' "$json" | grep -i title | head -1)"
		return
	fi
	row_pass "two commits updating alot and gitui to 0.28.2 titled \"$(jq -r '.result.title' "$json")\""
}
