# stages: quick full
# D-T2: a malformed config.toml, and one with an unknown key: each refused
# by name and line.
act() {
	printf 'worktrees = "%s"\n[check\non = 3\n' "$ROW_DIR" >"$ROW_DIR/bad.toml"
	printf 'worktrees = "%s"\nworktree_dir = "x"\n' "$ROW_DIR" >"$ROW_DIR/unknown.toml"
	(export DOCKHAND_CONFIG="$ROW_DIR/bad.toml"; dh_json status) || :
	(export DOCKHAND_CONFIG="$ROW_DIR/unknown.toml"; dh_json status) || :
}
assert() {
	local bad unknown
	bad=$(jq -r '.error // ""' "$ROW_DIR/json/1.json")
	unknown=$(jq -r '.error // ""' "$ROW_DIR/json/2.json")
	if ! printf '%s' "$bad" | grep -qE 'bad.toml.*(line|[0-9]+)'; then
		row_fail "malformed TOML not refused by file and line: $bad"
	elif ! printf '%s' "$unknown" | grep -q 'worktree_dir'; then
		row_fail "the unknown key not named: $unknown"
	else
		row_pass "refused: [$bad] [$unknown]"
	fi
}
