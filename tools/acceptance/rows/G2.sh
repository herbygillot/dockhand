# stages: full
# G2: check --keep in /opt/local: the branch's versions are left active,
# as port install leaves them, and the check names the snapshot that puts
# the prefix back, which the row then uses.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a check in /opt/local, as root" || return 0
	local_root || return 0
	local_active /opt/local >"$ROW_DIR/active.before"
	G2_BRANCH=$(run_name g2-keep)
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G2_BRANCH" --subject "rebuild to keep in /opt/local" || return 0
	dh check -b "$G2_BRANCH" --on local --keep </dev/null || :
	local_active /opt/local >"$ROW_DIR/active.kept"
	G2_SNAPSHOT=$(grep -o 'sudo port restore --snapshot-id [0-9]*' "$ROW_DIR/out.log" | tail -1 | awk '{print $NF}')
	[ -z "$G2_SNAPSHOT" ] || sudo /opt/local/bin/port -N restore --snapshot-id "$G2_SNAPSHOT" >>"$ROW_DIR/out.log" 2>&1 || :
}

assert() {
	[ -n "${G2_BRANCH:-}" ] || return 0
	local revision
	revision=$(grep -E "^  ${ACCEPT_GO_PORT} @" "$ROW_DIR/active.kept" | awk '{print $2}')
	[ -n "$G2_SNAPSHOT" ] || { row_fail "the kept check named no snapshot to put /opt/local back with"; return; }
	if [ "$(cat "$ROW_DIR/active.kept")" = "$(cat "$ROW_DIR/active.before")" ]; then
		row_fail "check --keep left the branch's ${ACCEPT_GO_PORT} inactive ($revision)"
		return
	fi
	local_same /opt/local "$ROW_DIR/active.before" "port restore of the kept check's snapshot didn't put /opt/local back" || return
	row_pass "check --keep left the branch's versions active, and its snapshot put /opt/local back"
}
