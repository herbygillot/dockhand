# stages: full
# A8: port uninstall dockhand with serve --install still in place: no
# launchd agent is left failing in a loop, or the uninstall says to run
# serve --uninstall first.

# a8_agent is serve's agent as launchctl list has it, "PID Status Label",
# PID "-" where it isn't running; nothing where no agent is loaded.
a8_agent() { launchctl list 2>/dev/null | awk '$3 == "io.github.herbygillot.dockhand.serve"' || :; }

# a8_runs is how many times launchd has started the agent, which a loop
# raises between two reads where its exit status may read the same.
a8_runs() { launchctl print "gui/$(id -u)/io.github.herbygillot.dockhand.serve" 2>/dev/null | awk '$1 == "runs" {print $3; exit}' || :; }

# a8_running says whether the agent last read has serve running.
a8_running() { [ -s "$ROW_DIR/agents" ] && [ "$(awk '{print $1}' "$ROW_DIR/agents")" != "-" ]; }

act() {
	host_only "port uninstall with the agent installed" || return 0
	allow_change "*"
	dh serve --install || :
	# The admin who runs port can't write dhtest's results, so the output
	# goes to the host's shared folder, and the row copies it in (the rc6
	# full stage).
	local log=${ACCEPT_HOST_ROOT:-/Users/Shared/dockhand-acceptance}/A8-uninstall.log
	checkpoint "uninstall dockhand: sudo port -N uninstall dockhand 2>&1 | tee $log" || return 0
	# serve stops a minute after its executable goes (ExecutableGrace), so
	# the agent is read once that's passed, for up to three minutes, until
	# serve isn't running; then again, to see it isn't started over. A
	# sample at 30 seconds caught serve inside its grace (the rc7 full
	# stage).
	local waited=0
	while :; do
		a8_agent >"$ROW_DIR/agents"
		if [ "$waited" -ge 90 ] && ! a8_running; then break; fi
		[ "$waited" -ge 180 ] && break
		sleep 10
		waited=$((waited + 10))
	done
	a8_runs >"$ROW_DIR/runs"
	sleep 20
	a8_agent >"$ROW_DIR/agents.again"
	a8_runs >"$ROW_DIR/runs.again"
	printf '%s\n' "$waited" >"$ROW_DIR/waited"
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
	local first again
	first=$(cat "$ROW_DIR/agents" 2>/dev/null) again=$(cat "$ROW_DIR/agents.again" 2>/dev/null)
	if [ -z "$first" ] && [ -z "$again" ]; then
		row_pass "no dockhand agent left after the uninstall"
	elif [ -n "$first" ] && [ "$first" = "$again" ] && [ "$(printf '%s' "$first" | awk '{print $1}')" = "-" ] &&
		[ "$(cat "$ROW_DIR/runs")" = "$(cat "$ROW_DIR/runs.again")" ]; then
		# Loaded but idle, its last exit and its count of runs the same
		# twenty seconds apart: serve stopped, and launchd doesn't start
		# it over.
		row_pass "serve stopped after the uninstall and isn't started again (launchctl: $first, after $(cat "$ROW_DIR/waited")s)"
	elif grep -qi 'serve --uninstall' "$ROW_DIR/uninstall.log" 2>/dev/null; then
		row_pass "the uninstall said to run serve --uninstall first"
	else
		row_fail "serve's agent is running or starting over: $first (runs $(cat "$ROW_DIR/runs" 2>/dev/null)), then $again (runs $(cat "$ROW_DIR/runs.again" 2>/dev/null))"
	fi
}
