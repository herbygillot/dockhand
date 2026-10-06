#!/usr/bin/env bash
# reset-user.sh makes the full stage's fresh user again before each run:
# it deletes the user dhtest, with its home, Keychain, launchd agents, and
# Tart images, and creates it anew (the project's
# plan/prime-time-environment.md). Because it deletes a macOS user, it
# refuses unless this Mac carries the test-host marker an admin writes,
# and unless the user is exactly dhtest. Without --run, it only says what
# it would do. It runs in two phases around dhtest's first login: --run
# deletes and makes dhtest, and stops for a person to log it in once;
# --run --after-login keeps dhtest and its home, which macOS made at that
# login, and sets up what's in it. --make-home has createhomedir make the
# home instead, and goes straight on, which froze the M1 at dhtest's
# first login (the Prime-time thread, 2026-10-06), so only on purpose.
#
#   tools/acceptance/host/reset-user.sh dhtest [--run] [--after-login | --make-home]
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
after_login=0 make_home=0
for arg in "$@"; do
	[ "$arg" = --after-login ] && after_login=1
	[ "$arg" = --make-home ] && make_home=1
done
[ "$after_login$make_home" != 11 ] || die "--after-login and --make-home are two ways to have dhtest's home: name one"

# as_dhtest runs a command as dhtest in dhtest's own home: sudo alone
# keeps the driver's HOME, and git config --global then wrote, or failed
# to lock, the driver's ~/.gitconfig (the Prime-time thread, 2026-10-05).
as_dhtest() { sudo -H -u dhtest "$@"; }

# The fork is cleared with the test account's token, which is checked
# before dhtest is deleted, so a missing login stops nothing halfway:
# GH_TOKEN, else the token file, else gh's own login.
: "${ACCEPT_GH_TOKEN_FILE:=$HOME/.dockhand-acceptance/gh-token}"
if [ -z "${GH_TOKEN:-}" ] && [ -r "$ACCEPT_GH_TOKEN_FILE" ]; then
	GH_TOKEN=$(tr -d '[:space:]' <"$ACCEPT_GH_TOKEN_FILE")
	export GH_TOKEN
fi
if command -v gh >/dev/null && [ -n "${ACCEPT_TEST_ACCOUNT:-}" ]; then
	sandbox="$ACCEPT_TEST_ACCOUNT/macports-ports"
	message=
	if ! gh auth status >/dev/null 2>&1; then
		message="gh has no login: set GH_TOKEN, or put the test account's token at $ACCEPT_GH_TOKEN_FILE (ACCEPT_GH_TOKEN_FILE)"
	elif [ "$(gh api "repos/$sandbox" --jq .permissions.push 2>/dev/null)" != true ]; then
		message="the token gh uses can't write to $sandbox: give it Contents, Pull requests, and Workflows, read and write, on the fork"
	elif [ "$DRY" = 1 ]; then
		echo "would check: the token gh uses can write to $sandbox's contents, by making a branch there and deleting it"
	else
		# The repository's push permission is the account's role, which a
		# fine-grained token without Contents write still reads as true,
		# and the sync then failed with 403 after dhtest was made again
		# (the Prime-time thread, 2026-10-05). Making a branch needs what
		# the sync does, and nothing else answers it.
		probe="refs/heads/dockhand-acceptance-probe-$$"
		head=$(gh api "repos/$sandbox/git/ref/heads/master" --jq .object.sha 2>/dev/null || :)
		if [ -z "$head" ] || ! gh api -X POST "repos/$sandbox/git/refs" -f ref="$probe" -f sha="$head" >/dev/null 2>&1; then
			message="the token gh uses can't write $sandbox's contents: give it Contents, Pull requests, and Workflows, read and write, on the fork"
		else
			gh api -X DELETE "repos/$sandbox/git/$probe" >/dev/null
		fi
	fi
	if [ -n "$message" ]; then
		if [ "$DRY" = 1 ]; then
			echo "would stop: $message"
		else
			die "$message"
		fi
	fi
fi

# 1. Delete dhtest and make it again. sysadminctl only assigns the home,
# which macOS makes at first login, and every step below writes in it, so
# the run stops here for that login. A home made beforehand, and filled
# before macOS's first-login setup, froze the Mac at that login.
if [ "$after_login" = 0 ]; then
	if id dhtest >/dev/null 2>&1; then
		step sudo sysadminctl -deleteUser dhtest
	fi
	step sudo sysadminctl -addUser dhtest -fullName "Dockhand Acceptance" -password - -home /Users/dhtest
	if [ "$make_home" = 1 ]; then
		step sudo createhomedir -c -u dhtest
	else
		human "log dhtest in at the login window once, switch back with fast user switching, and run $(basename "$0") dhtest --run --after-login"
	fi
fi
if [ "$DRY" = 0 ] && [ ! -d /Users/dhtest ]; then
	die "dhtest has no home: log dhtest in at the login window once, then run this with --after-login"
fi

# 2. FileVault stays on, so a person keeps dhtest logged in by fast user
# switching, for the serve rows. sysadminctl makes dhtest with the
# password read at its prompt, and says such a user can't unlock
# FileVault; after a reboot the driver unlocks it, and then logs dhtest in.

# 3. Git's identity and the test account's SSH key: the contributor's own
# setup, which dockhand doesn't do.
step as_dhtest git config --global user.name "Dockhand Acceptance"
step as_dhtest git config --global user.email "dhtest@example.invalid"
# The key goes where full.sh offers it alone, outside ~/.ssh, so no other
# key, and no agent's, stands in for it.
: "${ACCEPT_TEST_KEY:=$HOME/.dockhand-acceptance/herbyg-test_ed25519}"
if [ -r "$ACCEPT_TEST_KEY" ]; then
	step sudo install -d -o dhtest -m 700 /Users/dhtest/.dockhand-acceptance
	step sudo install -o dhtest -m 600 "$ACCEPT_TEST_KEY" /Users/dhtest/.dockhand-acceptance/test-account_ed25519
else
	human "put the test GitHub account's SSH key at $ACCEPT_TEST_KEY (ACCEPT_TEST_KEY), which this copies to dhtest"
fi

# 3b. dhtest's gh, logged in as the test account, with the token step 4
# uses: H3 counts that account's pull requests with it, and the rows close
# their test ones; never logged in, H3 passed while A4 opened one (the rc3
# full run, 2026-10-06). It's kept in gh's file, not a Keychain, which
# dhtest's isn't open to this session.
if [ -n "${ACCEPT_TEST_ACCOUNT:-}" ] && command -v gh >/dev/null; then
	token=${GH_TOKEN:-$(gh auth token 2>/dev/null || :)}
	if [ -z "$token" ]; then
		human "give this a token for $ACCEPT_TEST_ACCOUNT, as GH_TOKEN or at $ACCEPT_GH_TOKEN_FILE, for dhtest's gh"
	elif [ "$DRY" = 1 ]; then
		echo "would run: as_dhtest gh auth login --hostname github.com --with-token --insecure-storage, with $ACCEPT_TEST_ACCOUNT's token"
	else
		echo "+ as_dhtest gh auth login --hostname github.com --with-token --insecure-storage"
		printf '%s\n' "$token" | as_dhtest gh auth login --hostname github.com --with-token --insecure-storage
		login=$(as_dhtest gh api user --jq .login 2>/dev/null || :)
		[ "$login" = "$ACCEPT_TEST_ACCOUNT" ] || die "dhtest's gh is logged in as ${login:-nobody}, not $ACCEPT_TEST_ACCOUNT: the token is another account's"
	fi
else
	echo "skipped: dhtest's gh login needs gh and ACCEPT_TEST_ACCOUNT, the test GitHub login"
fi

# 4. The test account's fork, cleared. It's also the sandbox the full
# stage's test pull requests go to (DOCKHAND_PULL_REQUESTS), within it:
# leftover [testing] pull requests closed, there and at MacPorts;
# dockhand's branches deleted; and its master made MacPorts' again, so a
# pull request within it shows only its own commits.
if command -v gh >/dev/null && [ -n "${ACCEPT_TEST_ACCOUNT:-}" ]; then
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
		[ "$behind" = 0 ] || die "$sandbox's master is $behind commits behind MacPorts' after the sync: give the token gh uses Contents, Pull requests, and Workflows, read and write, on the fork, and run this again"
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
	step as_dhtest mkdir -p /Users/dhtest/Source
	if [ -d "$HOST_ROOT/ports-mirror.git" ]; then
		step as_dhtest git clone -q --reference "$HOST_ROOT/ports-mirror.git" --dissociate "https://github.com/$ACCEPT_TEST_ACCOUNT/macports-ports.git" "$tree"
	else
		step as_dhtest git clone -q "https://github.com/$ACCEPT_TEST_ACCOUNT/macports-ports.git" "$tree"
	fi
	step as_dhtest git -C "$tree" remote set-url --push origin "git@github.com:$ACCEPT_TEST_ACCOUNT/macports-ports.git"
	step as_dhtest git -C "$tree" remote add upstream https://github.com/macports/macports-ports.git
else
	echo "skipped: dhtest's ports clone needs ACCEPT_TEST_ACCOUNT, the test GitHub login"
fi

# 5. The starting point the harm sweep compares with.
step mkdir -p "$HOST_ROOT/baseline"
echo "free: $(df -g / | awk 'NR == 2 {print $4}') GB" | if [ "$DRY" = 1 ]; then sed 's/^/would record: /'; else tee "$HOST_ROOT/baseline/free"; fi
then_person "keep dhtest logged in, switching back with fast user switching; full.sh runs as dhtest"
echo "reset-user.sh: done$([ "$DRY" = 1 ] && echo ', as a dry run: nothing was changed')"
