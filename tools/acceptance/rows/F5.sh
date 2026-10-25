# stages: full
# F5: over SSH, with no one logged in at the screen: the login and token
# reads work or say the Keychain is locked, and with it locked setup offers
# no new login and says to unlock it (v0.6.0); Tart checks run;
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
	checkpoint "with no one logged in at the screen, ssh in as dhtest from another machine and run dockhand setup, dockhand check -b $F5_BRANCH --fresh, and dockhand serve --drain, saving their output in $ROW_DIR/ssh.log" || return 0
}
assert() {
	[ -s "$ROW_DIR/ssh.log" ] || { row_fail "no output from the SSH session"; return; }
	# With the Keychain locked, setup offers no new login, and its github
	# line and Next say to unlock it (v0.6.0, 6a5ad0f4): rc5's offered a
	# device login and named dockhand setup github, as v0.4.0's did.
	if grep -q 'Keychain is locked' "$ROW_DIR/ssh.log"; then
		if grep -q 'Log in to GitHub' "$ROW_DIR/ssh.log"; then
			row_fail "with the Keychain locked, setup offered a new GitHub login"
			return
		fi
		if ! grep -q '^Next: .*security unlock-keychain' "$ROW_DIR/ssh.log"; then
			row_fail "with the Keychain locked, setup's Next: didn't say to unlock it: $(grep '^Next:' "$ROW_DIR/ssh.log" | head -1)"
			return
		fi
		if grep -q '^Next: dockhand setup github' "$ROW_DIR/ssh.log"; then
			row_fail "with the Keychain locked, setup's Next: named dockhand setup github"
			return
		fi
	fi
	judged "over SSH the token was read or the Keychain was said locked, with setup offering no new login and saying to unlock it, the check ran, and notifications were skipped quietly"
}
