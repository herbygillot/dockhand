# shellcheck shell=bash
# What the host scripts share: a dry run unless --run, which says each
# step it would take, and the marker that says this Mac is the test host.

# HOST_ROOT holds what the acceptance test keeps on the host for every
# account: the overlay of ports, the pinned mirror, the staged Xcodes.
HOST_ROOT=${ACCEPT_HOST_ROOT:-/Users/Shared/dockhand-acceptance}

# The marker an admin writes, by hand, once the Mac is chosen as the test
# host: nothing that deletes a user runs without it.
HOST_MARKER="$HOST_ROOT/TEST-HOST"

DRY=1
for arg in "$@"; do
	[ "$arg" = --run ] && DRY=0
done

# step runs a command, or in a dry run says it would.
step() {
	if [ "$DRY" = 1 ]; then
		printf 'would run: %s\n' "$*"
	else
		printf '+ %s\n' "$*"
		"$@"
	fi
}

# human is a step only a person can take: it stops a real run with
# WAITING, and a dry run says it.
human() {
	printf 'WAITING: %s\n' "$*"
	[ "$DRY" = 1 ] || exit 75
}

die() {
	printf '%s: %s\n' "$(basename "$0")" "$*" >&2
	exit 1
}
