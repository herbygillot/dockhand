# stages: full
# A8: port uninstall dockhand with serve --install still in place: no
# launchd agent is left failing in a loop, or the uninstall says to run
# serve --uninstall first.
act() {
	host_only "port uninstall with the agent installed" || return 0
	allow_change "*"
	dh serve --install || :
	# The admin who runs port can't write dhtest's results, so the output
	# goes to the host's shared folder, and the row copies it in (the rc6
	# full stage).
	local log=${ACCEPT_HOST_ROOT:-/Users/Shared/dockhand-acceptance}/A8-uninstall.log
	checkpoint "uninstall dockhand: sudo port -N uninstall dockhand 2>&1 | tee $log" || return 0
	sleep 30
	launchctl print "gui/$(id -u)" 2>/dev/null | grep -i dockhand >"$ROW_DIR/agents" || :
	cp "$log" "$ROW_DIR/uninstall.log" 2>/dev/null || :
	# The rows after need dockhand, which nothing else puts back.
	checkpoint "install the candidate again: sudo port -N install dockhand" || return 0
	"$DH_BIN" --version >"$ROW_DIR/reinstalled" 2>&1 || :
}
# serve's agent goes when the row does: one left running would take the
# checks the rows after queue (the rc6 full stage).
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}

assert() {
	if ! grep -q . "$ROW_DIR/reinstalled" 2>/dev/null || ! "$DH_BIN" --version >/dev/null 2>&1; then
		row_fail "dockhand isn't installed again after the row, which the rows after need: $(head -1 "$ROW_DIR/reinstalled" 2>/dev/null)"
		return
	fi
	if [ ! -s "$ROW_DIR/agents" ]; then
		row_pass "no dockhand agent left after the uninstall"
	elif grep -qi 'serve --uninstall' "$ROW_DIR/uninstall.log" 2>/dev/null; then
		row_pass "the uninstall said to run serve --uninstall first"
	else
		row_fail "an agent is left: $(tr '\n' ' ' <"$ROW_DIR/agents")"
	fi
}
