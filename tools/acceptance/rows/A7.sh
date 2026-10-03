# stages: full
# A7: port upgrade dockhand to the next candidate while serve runs as a
# launchd agent. The running serve notices the new build or schema, then
# stops or restarts, saying so; the database is never stuck between
# versions.
act() {
	host_only "serve --install, then a port upgrade" || return 0
	allow_change "*"
	dh serve --install || return 0
	checkpoint "stage the next candidate and upgrade it: sudo port -N upgrade dockhand" || return 0
	sleep 60
	dh status || :
}
assert() {
	grep -qE '^\[exit 0\]' <(sed -n '/^\$ dockhand status/,/^\[exit/p' "$ROW_DIR/out.log") ||
		{ row_fail "status failed after the upgrade: the database may be stuck"; return; }
	judged "serve's log says it noticed the new build and stopped or restarted"
}
