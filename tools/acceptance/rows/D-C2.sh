# stages: full
# D-C2: auth logout while serve runs, then a new auth login: serve says
# once that it can't act, and picks up the new login without a restart.
act() {
	host_only "serve and the Keychain" || return 0
	dh serve --install || return 0
	login_keep || { row_result "not run" "dockhand has no login in the Keychain to log out of"; return 0; }
	dh auth logout || :
	sleep 120
	# The login comes back as it was, with no code to enter: serve reads
	# the Keychain at each look, so the item reappearing is the new login.
	login_restore || device_login "D-C2 logs dockhand back in while serve runs" || return 0
	sleep 420
	dh serve --uninstall || :
}
assert() {
	judged "serve's log says once that it can't act, then acts on the new login, with no restart"
}
