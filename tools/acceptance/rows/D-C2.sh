# stages: full
# D-C2: auth logout while serve runs, then a new auth login: serve says
# once that it can't act, and picks up the new login without a restart.
act() {
	host_only "serve and the Keychain" || return 0
	dh serve --install || return 0
	dh auth logout || :
	sleep 120
	checkpoint "log in again: dockhand auth login, entering the code in a browser" || return 0
	sleep 120
	dh serve --uninstall || :
}
assert() {
	judged "serve's log says once that it can't act, then acts on the new login, with no restart"
}
