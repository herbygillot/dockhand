#!/usr/bin/env bash
# reset-user.sh makes the full stage's fresh user again before each run:
# it deletes the user dhtest, with its home, Keychain, launchd agents, and
# Tart images, and creates it anew (the project's
# plan/prime-time-environment.md). Because it deletes a macOS user, it
# refuses unless this Mac carries the test-host marker an admin writes,
# and unless the user is exactly dhtest. Without --run, it only says what
# it would do.
#
#   tools/acceptance/host/reset-user.sh dhtest [--run]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"

user=${1:-}
[ "$user" = dhtest ] || die "name the user, dhtest: it resets only the full stage's own${user:+, not $user}"
if [ ! -f "$HOST_MARKER" ]; then
	die "$HOST_MARKER is missing: this Mac isn't marked as the test host, so no user is deleted"
fi
[ "$(id -un)" != dhtest ] || die "run it as the admin driver, not as dhtest"

# 1. Delete dhtest and make it again.
if id dhtest >/dev/null 2>&1; then
	step sudo sysadminctl -deleteUser dhtest
fi
step sudo sysadminctl -addUser dhtest -fullName "Dockhand Acceptance" -password - -home /Users/dhtest

# 2. FileVault stays on, so a person logs dhtest in, and keeps it logged in
# by fast user switching, for the serve rows.
human "log dhtest in at the login window once, then switch back with fast user switching"

# 3. Git's identity and the test account's SSH key: the contributor's own
# setup, which dockhand doesn't do.
step sudo -u dhtest git config --global user.name "Dockhand Acceptance"
step sudo -u dhtest git config --global user.email "dhtest@example.invalid"
human "put the test GitHub account's SSH key in /Users/dhtest/.ssh, readable by dhtest alone"

# 4. The test account's fork, cleared: leftover [testing] pull requests
# closed, and dockhand's branches deleted.
if command -v gh >/dev/null && [ -n "${ACCEPT_TEST_ACCOUNT:-}" ]; then
	for number in $(gh pr list --repo macports/macports-ports --author "$ACCEPT_TEST_ACCOUNT" --state open --json number,title --jq '.[] | select(.title | startswith("[testing]")) | .number'); do
		step gh pr close "$number" --repo macports/macports-ports
	done
	for ref in $(gh api "repos/$ACCEPT_TEST_ACCOUNT/macports-ports/git/matching-refs/heads/dockhand" --jq '.[].ref' 2>/dev/null); do
		step gh api -X DELETE "repos/$ACCEPT_TEST_ACCOUNT/macports-ports/git/$ref"
	done
else
	echo "skipped: clearing the fork needs gh and ACCEPT_TEST_ACCOUNT, the test GitHub login"
fi

# 5. The starting point the harm sweep compares with.
step mkdir -p "$HOST_ROOT/baseline"
echo "free: $(df -g / | awk 'NR == 2 {print $4}') GB" | if [ "$DRY" = 1 ]; then sed 's/^/would record: /'; else tee "$HOST_ROOT/baseline/free"; fi
echo "reset-user.sh: done$([ "$DRY" = 1 ] && echo ', as a dry run: nothing was changed')"
