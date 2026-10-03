#!/bin/sh
# The quick stage's GIT_SSH_COMMAND: Git's reads of the test account's fork
# over SSH go, with the test account's key alone, and its pushes never do.
# Git runs this as ssh, the host and the remote command last: a push is
# git-receive-pack, a read git-upload-pack. Submit's preview reads the
# fork's branch over its push URL, which a refusal of every connection
# refused too (the M1's run at 10aac0c3, D-S6).
for word in "$@"; do
	case "$word" in
	*git-receive-pack*)
		echo "the quick stage never pushes" >&2
		exit 1
		;;
	esac
done
if [ ! -r "${ACCEPT_GH_KEY:-}" ]; then
	echo "the quick stage reads the fork over SSH with the test account's key, ACCEPT_GH_KEY, which isn't there" >&2
	exit 1
fi
exec ssh -i "$ACCEPT_GH_KEY" -o IdentitiesOnly=yes -o IdentityAgent=none "$@"
