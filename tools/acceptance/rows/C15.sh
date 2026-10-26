# stages: quick full
# C15: a port's own holds (v0.6.0, Then's item 2; a620beff).
# [holds.ports."<port>"] with majors, moves, and after: config lists each
# key the port gives; a value none of them takes is refused by name; and
# submit's preview of the small Go port's update, after = a port with no
# pull request open at that version, says the port waits for it.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

# c15_config writes the stage's configuration with the port's holds after
# it, as the row's own, for one command.
c15_config() {
	cat "${DOCKHAND_CONFIG:?}" >"$ROW_DIR/c15.toml"
	printf '\n[holds.ports."%s"]\n%s\n' "$(port)" "$1" >>"$ROW_DIR/c15.toml"
}

setup() {
	dh_setup update "$(port)" --new || return 0
	C15_BRANCH=$(own_branch)
	dh_setup tidy -b "$C15_BRANCH" --yes || return 0
}

act() {
	[ -n "${C15_BRANCH:-}" ] || return 0
	c15_config 'majors = "allow"
moves = "follow"
after = ["acceptance-first"]'
	(export DOCKHAND_CONFIG="$ROW_DIR/c15.toml"; dh_json config) || :
	(export DOCKHAND_CONFIG="$ROW_DIR/c15.toml"; dh submit -b "$C15_BRANCH" --plan --no-check) || :
	c15_config 'moves = "always"'
	(export DOCKHAND_CONFIG="$ROW_DIR/c15.toml"; dh_json config) || :
}

assert() {
	[ -n "${C15_BRANCH:-}" ] || { row_fail "no branch to submit"; return; }
	local key value
	for key in majors moves after; do
		value=$(jq -r --arg k "holds.ports.\"$(port)\".$key" '.result.settings[]? | select(.key == $k) | .value' "$ROW_DIR/json/1.json")
		[ -n "$value" ] || { row_fail "config didn't list holds.ports.\"$(port)\".$key"; return; }
	done
	if ! grep -q "$(port) waits for acceptance-first's update to" "$ROW_DIR/out.log"; then
		row_fail "submit's preview didn't say the port waits for the one its holds name first"
		return
	fi
	if [ "$(cat "$ROW_DIR/json/2.json.exit")" = 0 ] || ! jq -r '.error // ""' "$ROW_DIR/json/2.json" | matches "holds.ports.\"$(port)\".moves: \"always\" is not hold or follow"; then
		row_fail "a moves value none takes wasn't refused by name: $(jq -r '.error // empty' "$ROW_DIR/json/2.json")"
		return
	fi
	row_pass "config lists the port's holds; submit's preview says it waits for the port named first; a bad value refused by name"
}
