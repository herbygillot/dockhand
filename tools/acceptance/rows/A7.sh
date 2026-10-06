# stages: full
# A7: port upgrade dockhand to the next candidate while serve runs as a
# launchd agent. The running serve notices its executable was replaced,
# says so, and stops once its checks end; launchd starts the new build,
# so a new serve leads, and status says no other build runs. The database
# is never stuck between versions.
act() {
	host_only "serve --install, then a port upgrade" || return 0
	allow_change "*"
	dh serve --install || return 0
	sleep 10
	# serve's process and how far its log went, before the upgrade.
	a7_pid >"$ROW_DIR/pid.before"
	wc -c <"$A7_LOG" 2>/dev/null | tr -d ' ' >"$ROW_DIR/log.before" || echo 0 >"$ROW_DIR/log.before"
	# There's a next candidate only while one is being made; on the newest
	# one, skip (the rc3 full stage).
	checkpoint "stage a dockhand newer than ${ACCEPT_CANDIDATE:-this candidate} in the overlay, as the next candidate or a build of main, and upgrade it: sudo port -N upgrade dockhand. With none to stage, answer skip" || return 0
	# serve stops once its checks end, and launchd starts the new build:
	# a new process leads.
	local waited=0
	while [ "$waited" -lt 300 ]; do
		a7_pid >"$ROW_DIR/pid.after"
		[ -s "$ROW_DIR/pid.after" ] && ! cmp -s "$ROW_DIR/pid.before" "$ROW_DIR/pid.after" && break
		sleep 10
		waited=$((waited + 10))
	done
	tail -c +"$(($(cat "$ROW_DIR/log.before") + 1))" "$A7_LOG" >"$ROW_DIR/serve.log" 2>/dev/null || :
	dh status || :
}

# A7_LOG is the agent's log, beside dhtest's database.
A7_LOG=$HOME/.dockhand/logs/serve.log

# a7_pid is the leading serve's process, or nothing.
a7_pid() { dh_quiet --json queue | jq -r '.result.serve_state.pid // empty'; }
# serve's agent goes when the row does, skipped or not: a skipped A7 left
# it running through the rows after (the rc3 full stage).
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}

assert() {
	grep -qE '^\[exit 0\]' <(sed -n '/^\$ dockhand status/,/^\[exit/p' "$ROW_DIR/out.log") ||
		{ row_fail "status failed after the upgrade: the database may be stuck"; return; }
	grep -q '^serve: dockhand was upgraded' "$ROW_DIR/serve.log" ||
		{ row_fail "serve's log says nothing of the upgrade: $(tail -3 "$ROW_DIR/serve.log" | tr '\n' ';')"; return; }
	grep -q '^serve: stopped for the upgrade' "$ROW_DIR/serve.log" ||
		{ row_fail "serve noticed the upgrade but didn't stop for it"; return; }
	if [ ! -s "$ROW_DIR/pid.after" ] || cmp -s "$ROW_DIR/pid.before" "$ROW_DIR/pid.after"; then
		row_fail "no new serve leads after the upgrade: pid $(cat "$ROW_DIR/pid.before") before, $(cat "$ROW_DIR/pid.after" 2>/dev/null) after"
		return
	fi
	if grep -q 'where this is' <(sed -n '/^\$ dockhand status/,/^\[exit/p' "$ROW_DIR/out.log"); then
		row_fail "status says serve runs another build than the upgraded one"
		return
	fi
	row_pass "serve said the upgrade, stopped, and a new serve (pid $(cat "$ROW_DIR/pid.after")) leads on the new build"
}
