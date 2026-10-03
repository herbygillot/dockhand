# stages: quick full
# F6: paths with spaces and non-ASCII characters: the database, the
# configuration, and the worktrees in a directory named so. Everything
# works, or is refused by name.
setup() {
	F6_DIR="$ROW_DIR/with space é"
	mkdir -p "$F6_DIR/worktrees"
	printf 'worktrees = "%s/worktrees"\n\n[cleanup]\nautomatic = false\n' "$F6_DIR" >"$F6_DIR/config.toml"
}
act() {
	(
		export DOCKHAND_DB="$F6_DIR/dockhand.db" DOCKHAND_CONFIG="$F6_DIR/config.toml"
		# setup has no --json yet; its exit is the row's to read.
		dh_setup setup -y || exit 0
		dh_json start f6 --port "${ACCEPT_GO_PORT:?}" || :
		dh_json status || :
	)
}
assert() {
	local n
	for n in 1 2; do
		[ "$(cat "$ROW_DIR/json/$n.json.exit" 2>/dev/null)" = 0 ] || { row_fail "$(cat "$ROW_DIR/json/$n.json.args") exited $(cat "$ROW_DIR/json/$n.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/$n.json")"; return; }
	done
	[ -f "$F6_DIR/worktrees/f6/_resources/port1.0/group/github-1.0.tcl" ] || find "$F6_DIR/worktrees/f6" -maxdepth 2 -name Portfile | grep -q . || { row_fail "the worktree under a spaced path is missing"; return; }
	row_pass "setup, start, and status under \"with space é\""
}
