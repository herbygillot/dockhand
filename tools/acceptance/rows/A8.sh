# stages: full
# A8: port uninstall dockhand with serve --install still in place: no
# launchd agent is left failing in a loop, or the uninstall says to run
# serve --uninstall first.
act() {
	host_only "port uninstall with the agent installed" || return 0
	allow_change "*"
	dh serve --install || :
	checkpoint "uninstall dockhand: sudo port -N uninstall dockhand, keeping its output in $ROW_DIR/uninstall.log" || return 0
	sleep 30
	launchctl print "gui/$(id -u)" 2>/dev/null | grep -i dockhand >"$ROW_DIR/agents" || :
}
assert() {
	if [ ! -s "$ROW_DIR/agents" ]; then
		row_pass "no dockhand agent left after the uninstall"
	elif grep -qi 'serve --uninstall' "$ROW_DIR/uninstall.log" 2>/dev/null; then
		row_pass "the uninstall said to run serve --uninstall first"
	else
		row_fail "an agent is left: $(tr '\n' ' ' <"$ROW_DIR/agents")"
	fi
}
