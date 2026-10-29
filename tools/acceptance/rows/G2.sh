# stages: full
# G2: check --keep in /opt/local: the branch's versions are left active,
# as port install leaves them, and the check names dockhand undo check-N,
# which puts the prefix back exactly, where port restore of its snapshot
# left unrequested ports inactive (rc8's G2, v0.6.0's known issue).
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a check in /opt/local, as root" || return 0
	local_root || return 0
	local_active /opt/local >"$ROW_DIR/active.before"
	G2_BRANCH=$(run_name g2-keep)
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G2_BRANCH" --subject "rebuild to keep in /opt/local" || return 0
	dh check -b "$G2_BRANCH" --on local --keep </dev/null || :
	local_active /opt/local >"$ROW_DIR/active.kept"
	G2_RUN=$(grep -o 'dockhand undo check-[0-9]*' "$ROW_DIR/out.log" | tail -1 | awk '{print $NF}')
	[ -z "$G2_RUN" ] || { local_root && dh undo "$G2_RUN" </dev/null; } || :
}

assert() {
	[ -n "${G2_BRANCH:-}" ] || return 0
	local revision
	revision=$(grep -E "^  ${ACCEPT_GO_PORT} @" "$ROW_DIR/active.kept" | awk '{print $2}')
	[ -n "$G2_RUN" ] || { row_fail "the kept check named no dockhand undo to put /opt/local back with"; return; }
	if [ "$(cat "$ROW_DIR/active.kept")" = "$(cat "$ROW_DIR/active.before")" ]; then
		row_fail "check --keep left the branch's ${ACCEPT_GO_PORT} inactive ($revision)"
		return
	fi
	local_same /opt/local "$ROW_DIR/active.before" "dockhand undo $G2_RUN didn't put /opt/local back exactly" || return
	# Its directory goes once the prefix is put back: the guest program,
	# root, gives what it wrote there back (G4 on rc8 left one).
	if grep -ls '"check":"'"$G2_RUN"'"' /var/tmp/dockhand-local-*/owner.json >/dev/null 2>&1; then
		row_fail "$G2_RUN's directory stayed in /var/tmp once /opt/local was put back: $(grep -ls '"check":"'"$G2_RUN"'"' /var/tmp/dockhand-local-*/owner.json | head -1)"
		return
	fi
	row_pass "check --keep left the branch's versions active, and dockhand undo $G2_RUN put /opt/local back exactly"
}
