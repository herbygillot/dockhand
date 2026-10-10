# stages: full
# F7: MacPorts set up its own way: a non-default prefix; sources.conf on
# the contributor's own checkout; a base newer than the guest's pinned
# one; port selfupdate during a check. Each found, said, or refused well.
# Its check names a branch of its own, made in setup: with earlier rows'
# branches set aside, check -p found none (the rc6 full stage).
setup() {
	host_only "MacPorts' configuration on the host" || return 0
	dh_setup update "${ACCEPT_GO_PORT:?}" --new || return 1
	F7_BRANCH=$(own_branch)
}
act() {
	host_only "MacPorts' configuration on the host" || return 0
	checkpoint "point sources.conf at $MACPORTS_TREE, then resume" || return 0
	# Plain output: rc10's providers has no --json (the rc10 rerun).
	dh providers || :
	dh_bg check -b "$F7_BRANCH"
	checkpoint "run sudo port selfupdate while the check runs" || return 0
	dh_bg_wait || :
}
assert() {
	judged "each MacPorts setup was found, said, or refused well, and the check survived selfupdate or said why not"
}
