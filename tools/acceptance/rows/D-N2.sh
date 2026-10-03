# stages: full
# D-N2: a distfile gone upstream, a port whose old tarball 404s. The
# comparison falls back to MacPorts' mirror, or says it couldn't compare,
# which holds.
act() {
	host_only "a port whose old distfile is gone upstream" || return 0
	checkpoint "name a port whose current distfile 404s upstream, with a newer release, in $ROW_DIR/port (termusic 0.9.1 was one)" || return 0
	dh_json update "$(cat "$ROW_DIR/port")" --plan || :
}
assert() {
	jq -e '.. | strings | select(test("mirror|couldn.t compare|not compared"))' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "fell back to the mirror, or said it couldn't compare" ||
		row_fail "neither the mirror nor a hold: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"
}
