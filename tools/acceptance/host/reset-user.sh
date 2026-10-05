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
# by fast user switching, for the serve rows. Nothing below needs the
# login, so it's said at the end rather than waited on here, which left
# steps 3 to 5 undone and a rerun deleting dhtest again (the Prime-time
# thread, 2026-10-05).

# 3. Git's identity and the test account's SSH key: the contributor's own
# setup, which dockhand doesn't do.
step sudo -u dhtest git config --global user.name "Dockhand Acceptance"
step sudo -u dhtest git config --global user.email "dhtest@example.invalid"
# The key goes where full.sh offers it alone, outside ~/.ssh, so no other
# key, and no agent's, stands in for it.
: "${ACCEPT_TEST_KEY:=$HOME/.dockhand-acceptance/herbyg-test_ed25519}"
if [ -r "$ACCEPT_TEST_KEY" ]; then
	step sudo install -d -o dhtest -m 700 /Users/dhtest/.dockhand-acceptance
	step sudo install -o dhtest -m 600 "$ACCEPT_TEST_KEY" /Users/dhtest/.dockhand-acceptance/test-account_ed25519
else
	human "put the test GitHub account's SSH key at $ACCEPT_TEST_KEY (ACCEPT_TEST_KEY), which this copies to dhtest"
fi

# 4. The test account's fork, cleared. It's also the sandbox the full
# stage's test pull requests go to (DOCKHAND_PULL_REQUESTS), within it:
# leftover [testing] pull requests closed, there and at MacPorts;
# dockhand's branches deleted; and its master made MacPorts' again, so a
# pull request within it shows only its own commits.
if command -v gh >/dev/null && [ -n "${ACCEPT_TEST_ACCOUNT:-}" ]; then
	sandbox="$ACCEPT_TEST_ACCOUNT/macports-ports"
	for repo in macports/macports-ports "$sandbox"; do
		for number in $(gh pr list --repo "$repo" --author "$ACCEPT_TEST_ACCOUNT" --state open --json number,title --jq '.[] | select(.title | startswith("[testing]")) | .number'); do
			step gh pr close "$number" --repo "$repo"
		done
	done
	for prefix in dockhand dockhand-check; do
		for ref in $(gh api "repos/$sandbox/git/matching-refs/heads/$prefix/" --jq '.[].ref' 2>/dev/null); do
			step gh api -X DELETE "repos/$sandbox/git/$ref"
		done
	done
	step gh repo sync "$sandbox" --branch master --force || :
	# Syncing, and deleting branches, need the test token's write access
	# to the fork: Contents and Pull requests, read and write. Without it
	# the fork's master drifts behind MacPorts' (107 commits on
	# 2026-10-05), and its test pull requests show MacPorts' commits as
	# theirs. GitHub says how far behind it is, which a sync leaves at 0.
	if [ "$DRY" = 0 ]; then
		behind=$(gh api "repos/$sandbox/compare/master...macports:macports-ports:master" --jq '.ahead_by' 2>/dev/null || echo unknown)
		[ "$behind" = 0 ] || die "$sandbox's master is $behind commits behind MacPorts' after the sync: give the token gh uses Contents and Pull requests, read and write, on the fork, and run this again"
	fi
else
	echo "skipped: clearing the fork needs gh and ACCEPT_TEST_ACCOUNT, the test GitHub login"
fi

# 4b. dhtest's ports clone, MACPORTS_TREE as full.sh has it: the test
# account's fork, read over HTTPS and pushed to over SSH with the test key
# (full.sh's GIT_SSH_COMMAND), MacPorts' own as the upstream remote. It
# borrows the host's mirror's objects while it clones, and keeps none of
# its own ties to it.
if [ -n "${ACCEPT_TEST_ACCOUNT:-}" ]; then
	tree=/Users/dhtest/Source/macports-ports
	step sudo -u dhtest mkdir -p /Users/dhtest/Source
	if [ -d "$HOST_ROOT/ports-mirror.git" ]; then
		step sudo -u dhtest git clone -q --reference "$HOST_ROOT/ports-mirror.git" --dissociate "https://github.com/$ACCEPT_TEST_ACCOUNT/macports-ports.git" "$tree"
	else
		step sudo -u dhtest git clone -q "https://github.com/$ACCEPT_TEST_ACCOUNT/macports-ports.git" "$tree"
	fi
	step sudo -u dhtest git -C "$tree" remote set-url --push origin "git@github.com:$ACCEPT_TEST_ACCOUNT/macports-ports.git"
	step sudo -u dhtest git -C "$tree" remote add upstream https://github.com/macports/macports-ports.git
else
	echo "skipped: dhtest's ports clone needs ACCEPT_TEST_ACCOUNT, the test GitHub login"
fi

# 5. The starting point the harm sweep compares with.
step mkdir -p "$HOST_ROOT/baseline"
echo "free: $(df -g / | awk 'NR == 2 {print $4}') GB" | if [ "$DRY" = 1 ]; then sed 's/^/would record: /'; else tee "$HOST_ROOT/baseline/free"; fi
then_person "log dhtest in at the login window once, then switch back with fast user switching; full.sh runs as dhtest after that"
echo "reset-user.sh: done$([ "$DRY" = 1 ] && echo ', as a dry run: nothing was changed')"
