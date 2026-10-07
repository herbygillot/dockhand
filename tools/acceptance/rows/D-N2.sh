# stages: full
# D-N2: a distfile gone upstream, a port whose old tarball 404s. The
# comparison falls back to MacPorts' mirror, or says it couldn't compare,
# which holds.
#
# It needs such a port, with a newer release, named as ACCEPT_DN2_PORT:
# none can be made, since dockhand finds releases on GitHub's API, and a
# hint goes stale (termusic 0.9.1 was one; MacPorts has since moved on,
# and its URLs answer). Without one, the row isn't run (the rc6 full
# stage).
act() {
	host_only "a port whose old distfile is gone upstream" || return 0
	if [ -z "${ACCEPT_DN2_PORT:-}" ]; then
		row_result "not run" "no port whose current distfile 404s upstream, with a newer release, was named as ACCEPT_DN2_PORT"
		return 0
	fi
	dh_json update "$ACCEPT_DN2_PORT" --new --plan || :
}
assert() {
	jq -e '.. | strings | select(test("mirror|couldn.t compare|not compared"))' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "fell back to the mirror, or said it couldn't compare" ||
		row_fail "neither the mirror nor a hold: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"
}
