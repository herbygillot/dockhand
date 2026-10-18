# stages: full
# B7: serve --install left running overnight, across the 8-hour token
# expiry and the daily look at serve.outdated_at: the look runs, PR states
# refresh, the login renews without a prompt, notifications arrive, and
# serve --uninstall removes it cleanly. C1's renewal is read here too.
act() {
	host_only "serve as a launchd agent overnight" || return 0
	allow_change "*"
	dh setup || :
	dh serve --install || return 0
	checkpoint "leave serve running overnight, past the token's 8-hour expiry and serve.outdated_at; note whether notifications arrived in $ROW_DIR/notes" || return 0
	dh setup || :
	dh status || :
	# serve has no --json; dh_json made rc4 refuse the uninstall (the rc4
	# batch).
	dh serve --uninstall || :
	# launchd unloads the agent after bootout returns, a few seconds on:
	# read at once, rc6's and rc10's runs found it still there. --uninstall
	# waits until launchd has let it go, so it's read once, at once.
	launchctl print "gui/$(id -u)" 2>/dev/null | grep -i dockhand >"$ROW_DIR/agents" || :
}
# serve's agent goes when the row does, skipped or not, as A7's and A8's
# do (the rc6 full stage).
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}

assert() {
	[ ! -s "$ROW_DIR/agents" ] || { row_fail "serve --uninstall returned with launchd still holding its agent"; return; }
	judged "the daily look ran, PR states refreshed, the login renewed without a prompt, and notifications arrived"
}
