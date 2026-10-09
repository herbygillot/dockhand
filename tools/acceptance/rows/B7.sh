# stages: full
# B7: serve --install left running overnight, across the 8-hour token
# expiry and the daily look at serve.outdated_at: the look runs, PR states
# refresh, the login renews without a prompt, notifications arrive, and
# serve --uninstall removes it cleanly. C1's renewal is read here too.
act() {
	host_only "serve as a launchd agent overnight" || return 0
	allow_change "*"
	dh auth status || :
	dh serve --install || return 0
	checkpoint "leave serve running overnight, past the token's 8-hour expiry and serve.outdated_at; note whether notifications arrived in $ROW_DIR/notes" || return 0
	dh auth status || :
	dh status || :
	dh_json serve --uninstall || :
	# launchd unloads the agent after bootout returns, a few seconds on:
	# read at once, rc6's and rc10's runs found it still there. It's read
	# until it's gone, up to thirty seconds.
	local waited=0
	while launchctl print "gui/$(id -u)" 2>/dev/null | grep -i dockhand >"$ROW_DIR/agents" && [ "$waited" -lt 30 ]; do
		sleep 2
		waited=$((waited + 2))
	done
	printf '%s\n' "$waited" >"$ROW_DIR/agents.waited"
}
# serve's agent goes when the row does, skipped or not, as A7's and A8's
# do (the rc6 full stage).
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}

assert() {
	[ ! -s "$ROW_DIR/agents" ] || { row_fail "serve --uninstall left an agent, there after $(cat "$ROW_DIR/agents.waited" 2>/dev/null)s"; return; }
	judged "the daily look ran, PR states refreshed, the login renewed without a prompt, and notifications arrived"
}
