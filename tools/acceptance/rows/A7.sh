# stages: full
# A7: port upgrade dockhand to the next candidate while serve runs as a
# launchd agent. The running serve notices the new build or schema, then
# stops or restarts, saying so; the database is never stuck between
# versions.
act() {
	host_only "serve --install, then a port upgrade" || return 0
	allow_change "*"
	dh serve --install || return 0
	# There's a next candidate only while one is being made; on the newest
	# one, skip (the rc3 full stage).
	checkpoint "stage a dockhand newer than ${ACCEPT_CANDIDATE:-this candidate} in the overlay, as the next candidate or a build of main, and upgrade it: sudo port -N upgrade dockhand. With none to stage, answer skip" || return 0
	sleep 60
	dh status || :
}
# serve's agent goes when the row does, skipped or not: a skipped A7 left
# it running through the rows after (the rc3 full stage).
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}

assert() {
	grep -qE '^\[exit 0\]' <(sed -n '/^\$ dockhand status/,/^\[exit/p' "$ROW_DIR/out.log") ||
		{ row_fail "status failed after the upgrade: the database may be stuck"; return; }
	judged "serve's log says it noticed the new build and stopped or restarted"
}
