# stages: full
# F3: remote layouts: SSH and HTTPS fork remotes; the fork as origin with
# MacPorts as upstream, and the reverse; a fork renamed on GitHub. The
# fork and upstream are found every time; providers.github.remote settles
# any ambiguity.
act() {
	host_only "the test account's fork" || return 0
	local layout
	for layout in ssh https swapped; do
		checkpoint "set the remotes as the $layout layout in $MACPORTS_TREE" || return 0
		dh_json auth status || :
		dh_json submit --plan || :
	done
	checkpoint "rename the fork on GitHub, then put the remotes back" || return 0
	dh_json auth status || :
}
assert() {
	judged "the fork and upstream were found in each layout and after the rename"
}
