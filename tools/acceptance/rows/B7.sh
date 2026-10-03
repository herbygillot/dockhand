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
	launchctl print "gui/$(id -u)" 2>/dev/null | grep -i dockhand >"$ROW_DIR/agents" || :
}
assert() {
	[ ! -s "$ROW_DIR/agents" ] || { row_fail "serve --uninstall left an agent"; return; }
	judged "the daily look ran, PR states refreshed, the login renewed without a prompt, and notifications arrived"
}
