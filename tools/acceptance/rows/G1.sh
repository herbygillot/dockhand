# stages: full
# G1: a check in /opt/local, as-is (v0.6.0's local provider): a one-port
# and a two-port branch, each built among the ports already active, the
# check recorded with how many were, and the active ports put back as
# they were.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a check in /opt/local, as root" || return 0
	local_root || return 0
	local_active /opt/local >"$ROW_DIR/active.before"
	G1_ONE=$(run_name g1-one)
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G1_ONE" --subject "rebuild to check in /opt/local" || return 0
	dh_json check -b "$G1_ONE" --on local </dev/null || :
	local_active /opt/local >"$ROW_DIR/active.one"
	G1_TWO=$(run_name g1-two)
	dh_setup revbump "$ACCEPT_GO_PORT" "${ACCEPT_RUST_PORT:?}" --new="$G1_TWO" --subject "rebuild two to check in /opt/local" || return 0
	dh_json check -b "$G1_TWO" --on local </dev/null || :
}

assert() {
	[ -n "${G1_TWO:-}" ] || return 0
	local n file
	for n in 1 2; do
		file=$ROW_DIR/json/$n.json
		if [ "$(cat "$file.exit")" != 0 ]; then
			row_fail "check $n in /opt/local exited $(cat "$file.exit"): $(jq -r '.error // ""' "$file")"
			return
		fi
	done
	[ "$(cat "$ROW_DIR/active.one")" = "$(cat "$ROW_DIR/active.before")" ] || { row_fail "the one-port check left /opt/local's active ports changed"; return; }
	local_same /opt/local "$ROW_DIR/active.before" "the two-port check left /opt/local's active ports changed" || return
	grep -q 'with [0-9]* ports* active' <(dh submit -b "$G1_TWO" --plan --no-check 2>&1) ||
		{ row_fail "submit's preview's Tested on line didn't say how many ports were active in /opt/local"; return; }
	row_pass "both branches built in /opt/local among its active ports, put back as they were, and Tested on names how many were active"
}
