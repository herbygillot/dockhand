# stages: full
# F5: over SSH, with no one logged in at the screen: auth login and token
# reads work or say the Keychain is locked; Tart checks run;
# notifications are skipped quietly.
# Its check names a branch of its own, made in setup: with earlier rows'
# branches set aside, check -p found none (the rc6 full stage). It's
# --fresh, so Tart starts a VM over SSH: on rc10 it reused check-132's
# build and started none.
setup() {
	host_only "an SSH session to the host" || return 0
	dh_setup update "${ACCEPT_GO_PORT:?}" --new || return 1
	F5_BRANCH=$(own_branch)
}
act() {
	host_only "an SSH session to the host" || return 0
	checkpoint "with no one logged in at the screen, ssh in as dhtest from another machine and run dockhand auth status, dockhand check -b $F5_BRANCH --fresh, and dockhand serve --drain, saving their output in $ROW_DIR/ssh.log" || return 0
}
assert() {
	[ -s "$ROW_DIR/ssh.log" ] || { row_fail "no output from the SSH session"; return; }
	judged "over SSH the token was read or the Keychain was said locked, the check ran, and notifications were skipped quietly"
}
