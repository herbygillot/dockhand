# stages: full
# F3: remote layouts: SSH and HTTPS fork remotes; the fork as origin with
# MacPorts as upstream, and the reverse; a fork renamed on GitHub. The
# fork and upstream are found every time; providers.github.remote settles
# any ambiguity.
#
# Its submit names a branch of its own: on the ports checkout's master,
# with earlier rows' branches set aside, submit refused for want of one
# before it looked for the fork (the rc6 full stage).
setup() {
	host_only "the test account's fork" || return 0
	dh_setup update "${ACCEPT_GO_PORT:?}" --new || return 1
	F3_BRANCH=$(own_branch)
	dh tidy -b "$F3_BRANCH" -y || return 1
}
act() {
	host_only "the test account's fork" || return 0
	local layout
	for layout in ssh https swapped; do
		checkpoint "set the remotes as the $layout layout in $MACPORTS_TREE" || return 0
		dh_json auth status || :
		# --no-check: setup makes no check, and the preview judged here is
		# the fork's and upstream's, not the check's.
		dh_json submit -b "$F3_BRANCH" --plan --no-check || :
	done
	checkpoint "rename the fork on GitHub, then put the remotes back as they were, still naming its old name" || return 0
	dh_json auth status || :
	# submit, which finds the fork, refuses one GitHub renamed, naming the
	# new name and the git remote set-url to run, where it read "invalid
	# or archived" (the rc8 full stage).
	dh_json submit -b "$F3_BRANCH" --plan --no-check || :
	F3_RENAMED=$DH_LAST_JSON
	checkpoint "rename the fork back on GitHub" || return 0
}
assert() {
	if [ -n "${F3_RENAMED:-}" ]; then
		jq -r '.error // ""' "$F3_RENAMED" | grep -q 'is now .* on GitHub, renamed or transferred.*git remote set-url' ||
			{ row_fail "submit didn't name the renamed fork and the remote to point at it: $(jq -r '.error // "it went on"' "$F3_RENAMED")"; return; }
	fi
	judged "the fork and upstream were found in each layout, and submit named the rename"
}
