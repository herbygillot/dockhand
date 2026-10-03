# stages: full
# F7: MacPorts set up its own way: a non-default prefix; sources.conf on
# the contributor's own checkout; a base newer than the guest's pinned
# one; port selfupdate during a check. Each found, said, or refused well.
act() {
	host_only "MacPorts' configuration on the host" || return 0
	checkpoint "point sources.conf at $MACPORTS_TREE, then resume" || return 0
	dh_json providers || :
	dh_bg check -p "${ACCEPT_GO_PORT:?}"
	checkpoint "run sudo port selfupdate while the check runs" || return 0
	dh_bg_wait || :
}
assert() {
	judged "each MacPorts setup was found, said, or refused well, and the check survived selfupdate or said why not"
}
