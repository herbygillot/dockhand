# stages: full
# F5: over SSH, with no one logged in at the screen: auth login and token
# reads work or say the Keychain is locked; Tart checks run;
# notifications are skipped quietly.
act() {
	host_only "an SSH session to the host" || return 0
	checkpoint "with no one logged in at the screen, ssh in as dhtest from another machine and run dockhand auth status, dockhand check -p $ACCEPT_GO_PORT, and dockhand serve --drain, saving their output in $ROW_DIR/ssh.log" || return 0
}
assert() {
	[ -s "$ROW_DIR/ssh.log" ] || { row_fail "no output from the SSH session"; return; }
	judged "over SSH the token was read or the Keychain was said locked, the check ran, and notifications were skipped quietly"
}
