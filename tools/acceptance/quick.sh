#!/usr/bin/env bash
# quick.sh runs the quick stage (the project's plan/prime-time.md): it
# builds the dockhand under test from this checkout, sets up a fresh
# scratch environment, and runs the quick stage's rows there.
#
#   tools/acceptance/quick.sh [--candidate <rc>] [--rows "A3 B1"]
#   tools/acceptance/quick.sh --image     # once: the stage's Tart image
#   tools/acceptance/quick.sh --dry-full [--rows ...]
#                                         # the full stage's rows, each up
#                                         # to its first step for the host
#
# The environment lives in ACCEPT_STATE, ~/.dockhand-acceptance/quick
# unless set:
#   - its own database, configuration, caches, and worktrees;
#   - a scratch ports clone whose master, upstream.git, is pinned at
#     ACCEPT_PIN, a fixed older commit, so the same ports are always due
#     and two runs compare;
#   - a local bare fork, fork.git, so nothing can be pushed anywhere real;
#     and, where ACCEPT_GH_FORK names the test account's fork on GitHub,
#     such as herbyg-test/macports-ports, a remote "github" for it, which
#     submit's preview finds as the fork, as the stage's token's login
#     owns it. The stage never pushes there: its GIT_SSH_COMMAND refuses
#     every connection, so a push fails before it leaves the Mac.
#   - its own Tart homes, dockhand's (DOCKHAND_TART_HOME), its SSH keys
#     (DOCKHAND_SSH_DIR) and Tart's (TART_HOME), under tart/, with the one
#     image the check rows build in, which --image makes once.
#   - a GitHub token that reads only: ACCEPT_GH_TOKEN, or the file
#     ACCEPT_GH_TOKEN_FILE names, ~/.dockhand-acceptance/gh-token unless
#     set, made on GitHub with no scopes. dockhand reads it as GH_TOKEN,
#     before your own login, so the rows that ask GitHub get its limit of
#     5,000 requests an hour, not the 60 a request without one gets, and
#     your login is neither read nor renewed. Without one, quick.sh says
#     so: those rows run into the limit, and dockhand may read your login.
#     GitHub's limit is the account's, so a token of the test account
#     keeps the stage's requests out of what your own work spends.
# The pinned upstream borrows the objects of ACCEPT_PORTS_SOURCE, your
# ports clone, which is only read: a gc --prune or a fresh clone there
# can drop objects upstream.git needs, and quick.sh then makes it again.
# Everything but upstream.git, tart/, the port index cache, and the
# results are made afresh each run. Nothing runs against your own
# database, configuration, or Tart homes: lib/guard.sh refuses that, and
# the harm sweep fails a row that changes ~/.dockhand, ~/.tart or ~/.ssh.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
: "${ACCEPT_STATE:=$HOME/.dockhand-acceptance/quick}"
: "${ACCEPT_PORTS_SOURCE:=$HOME/Source/macports-ports}"
: "${ACCEPT_PIN:=026b3878a258b85619633709d89871f2f8750b94}"
# The ports the rows update: a small Go port and a small Rust port that
# the pin has at an older release than upstream's.
: "${ACCEPT_GO_PORT:=go-critic}"
: "${ACCEPT_RUST_PORT:=dust}"
export ACCEPT_GO_PORT ACCEPT_RUST_PORT ACCEPT_REPO="$repo"
mkdir -p "$ACCEPT_STATE"
state=$(cd "$ACCEPT_STATE" && pwd -P)
export ACCEPT_STATE=$state

source_objects=$(git -C "$ACCEPT_PORTS_SOURCE" rev-parse --path-format=absolute --git-common-dir)/objects
if ! git -C "$ACCEPT_PORTS_SOURCE" cat-file -e "$ACCEPT_PIN^{commit}" 2>/dev/null; then
	echo "quick.sh: $ACCEPT_PORTS_SOURCE hasn't got $ACCEPT_PIN; fetch MacPorts' master there, or set ACCEPT_PIN" >&2
	exit 2
fi

# The pinned upstream, kept between runs: an empty bare repository that
# borrows the ports clone's objects, its master at the pin. One whose
# borrowed objects are gone is made again.
if [ -d "$state/upstream.git" ] && ! git -C "$state/upstream.git" cat-file -e "$ACCEPT_PIN^{tree}" 2>/dev/null; then
	echo "quick.sh: upstream.git has lost objects it borrowed from $ACCEPT_PORTS_SOURCE; making it again" >&2
	rm -rf "$state/upstream.git"
fi
if [ ! -d "$state/upstream.git" ]; then
	git init -q --bare -b master "$state/upstream.git"
	printf '%s\n' "$source_objects" >"$state/upstream.git/objects/info/alternates"
fi

export DOCKHAND_DB="$state/home/dockhand.db" DOCKHAND_CONFIG="$state/home/config.toml"
export MACPORTS_TREE="$state/clone" DOCKHAND_UPSTREAM="$state/upstream.git"
# The port index cache is kept between runs, since an index is the
# pinned tree's, and building one takes minutes; the readings aren't.
mkdir -p "$state/cache/index"
export DOCKHAND_INDEX_CACHE="$state/cache/index" DOCKHAND_READING_CACHE="$state/home/readings"
# The github remote, where there is one, is read, never pushed to: SSH,
# which a push to it takes, is refused outright, and no key, the agent's
# included, is offered. H2 watches fork.git.
export ACCEPT_WATCH="$state/clone" ACCEPT_UPSTREAM="origin github" ACCEPT_RUN_DIR="$state/clone"
export GIT_SSH_COMMAND="sh -c 'echo \"the quick stage never pushes\" >&2; exit 1' --"
# The Tart homes, kept between runs, since an image takes an hour and
# tens of gigabytes to make.
mkdir -p "$state/tart"
export DOCKHAND_TART_HOME="$state/tart/dockhand" DOCKHAND_SSH_DIR="$state/tart/ssh" TART_HOME="$state/tart/tart"
# The stage's token, which reads only.
: "${ACCEPT_GH_TOKEN_FILE:=$HOME/.dockhand-acceptance/gh-token}"
if [ -z "${ACCEPT_GH_TOKEN:-}" ] && [ -r "$ACCEPT_GH_TOKEN_FILE" ]; then
	# A token others on the Mac can read is said, not used quietly.
	case "$(stat -f %Lp "$ACCEPT_GH_TOKEN_FILE")" in
	600 | 400) ;;
	*) echo "quick.sh: $ACCEPT_GH_TOKEN_FILE can be read by others on this Mac; chmod 600 it" >&2 ;;
	esac
	ACCEPT_GH_TOKEN=$(tr -d '[:space:]' <"$ACCEPT_GH_TOKEN_FILE")
fi
if [ -n "${ACCEPT_GH_TOKEN:-}" ]; then
	export GH_TOKEN=$ACCEPT_GH_TOKEN
	unset GITHUB_TOKEN
else
	echo "quick.sh: no GitHub token for the stage (ACCEPT_GH_TOKEN, or $ACCEPT_GH_TOKEN_FILE): rows that ask GitHub will run into its limit of 60 requests an hour, and dockhand may read your own login; make a token with no scopes and put it there" >&2
fi
unset ACCEPT_GH_TOKEN
export ACCEPT_SECRET_DIRS="$state/home"
export ACCEPT_HOME_DIRS="$HOME/.dockhand $HOME/.tart $HOME/.ssh"
unset ACCEPT_GH_LOGIN

# fresh makes the rest afresh: the fork, the clone, dockhand's own state,
# registered with setup. The runner does it again before each row, so a
# row starts from the pin, whatever the one before it left.
fresh() {
	git -C "$state/upstream.git" update-ref refs/heads/master "$ACCEPT_PIN"
	rm -rf "$state/fork.git" "$state/clone" "$state/home" "$state/worktrees"
	git init -q --bare -b master "$state/fork.git"
	printf '%s\n' "$source_objects" >"$state/fork.git/objects/info/alternates"
	git clone -q --shared --no-checkout "$state/upstream.git" "$state/clone"
	git -C "$state/clone" sparse-checkout set --cone _resources
	git -C "$state/clone" checkout -q master
	git -C "$state/clone" remote add fork "$state/fork.git"
	if [ -n "${ACCEPT_GH_FORK:-}" ]; then
		# Read over HTTPS, which a public fork answers without a login;
		# pushed to only over SSH, which GIT_SSH_COMMAND refuses, so no
		# credential helper's login can push there either.
		git -C "$state/clone" remote add github "https://github.com/$ACCEPT_GH_FORK.git"
		git -C "$state/clone" remote set-url --push github "git@github.com:$ACCEPT_GH_FORK.git"
	fi
	git -C "$state/clone" config user.name "Dockhand Acceptance"
	git -C "$state/clone" config user.email acceptance@example.invalid
	mkdir -p "$state/home" "$state/worktrees"
	cat >"$state/home/config.toml" <<TOML
worktrees = "$state/worktrees"

[cleanup]
automatic = false
TOML
	"$DH_BIN" setup -y >"$state/home/setup.log" 2>&1 || {
		echo "quick.sh: dockhand setup failed in the scratch environment:" >&2
		cat "$state/home/setup.log" >&2
		exit 1
	}
}

if [ "${1:-}" = --reset ]; then
	: "${DH_BIN:?quick.sh --reset is the runner's, with the stage's environment}"
	fresh
	exit 0
fi

# The dockhand under test, built from this checkout.
if [ -z "${DH_BIN:-}" ]; then
	rm -rf "$state/bin"
	mkdir -p "$state/bin"
	(cd "$repo" && make -s build BINARY="$state/bin/dockhand")
	DH_BIN="$state/bin/dockhand"
fi
# The same, with the acceptance build's failpoints, for the kill rows.
if [ -z "${DH_FAILPOINT_BIN:-}" ]; then
	(cd "$repo" && GOFLAGS=-mod=vendor go build -tags acceptance -o "$state/bin/dockhand-failpoints" ./cmd/dockhand)
	DH_FAILPOINT_BIN="$state/bin/dockhand-failpoints"
fi
export DH_BIN DH_FAILPOINT_BIN
fresh
if [ "${1:-}" = --image ]; then
	exec "$DH_BIN" providers setup tart
fi
if ! ls -d "$DOCKHAND_TART_HOME"/vms/dockhand-base-* >/dev/null 2>&1; then
	echo "quick.sh: the stage's Tart home, $DOCKHAND_TART_HOME, has no image, and the check rows build in one;" >&2
	echo "  make it once with tools/acceptance/quick.sh --image, which downloads macOS's vanilla image" >&2
	exit 2
fi
# D-R2 takes both VM slots with two clones of a vanilla image, as a
# person's own VMs, in the stage's own TART_HOME: unless ACCEPT_VANILLA
# names one, the vanilla image dockhand's setup started from, which that
# home pulls once and keeps.
if [ -z "${ACCEPT_VANILLA:-}" ] && command -v tart >/dev/null; then
	ACCEPT_VANILLA=$(TART_HOME=$DOCKHAND_TART_HOME tart list --source oci --format json 2>/dev/null |
		jq -r '[.[] | .Name | select(test("cirruslabs/macos-.*-vanilla"))][0] // empty')
fi
export ACCEPT_VANILLA
export ACCEPT_RESET="$here/quick.sh --reset"
if [ "${1:-}" = --dry-full ]; then
	shift
	exec "$here/run.sh" --stage full --dry-run --candidate "dry-$(git -C "$repo" rev-parse --short HEAD)" "$@"
fi
exec "$here/run.sh" --stage quick "$@"
