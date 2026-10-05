#!/usr/bin/env bash
# full.sh runs the full stage (the project's plan/prime-time.md) on the
# test host, as the fresh user dhtest that host/reset-user.sh made, with
# the candidate host/stage-candidate.sh put in the overlay:
#
#   tools/acceptance/full.sh --candidate v0.3.0-rcN [--rows "A1 A2"]
#
# Nothing here is scratch: the rows act on dhtest's own ports clone, its
# dockhand as row A1 installs it, and the test GitHub account, and lib/
# guard.sh refuses any other user. The run stops at each human checkpoint
# with WAITING, which tools/acceptance/resume.sh answers; before the first
# row, the pull requests it means to open wait on the person's approval.
#
# What it needs from the driver:
#   ACCEPT_GH_LOGIN    the test GitHub account's login, whose PRs H3 counts
#   ACCEPT_GO_PORT     a small Go port that's due, and ACCEPT_RUST_PORT a
#                      small Rust one, chosen on the day
#   ACCEPT_GH_KEY      the test account's SSH key, which git offers alone,
#                      ~/.dockhand-acceptance/test-account_ed25519 unless
#                      set: never the agent's keys, which may be a person's
#   ACCEPT_SANDBOX     where test pull requests go, within it: the test
#                      account's fork, <login>/macports-ports unless set.
#                      Only the pull requests the person marks real on the
#                      approval list go to MacPorts.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# dhtest's login shell doesn't put MacPorts on PATH; the rows run port,
# and the dockhand row A1 installs (the Prime-time thread, 2026-10-05).
case ":$PATH:" in *:/opt/local/bin:*) ;; *) export PATH="/opt/local/bin:/opt/local/sbin:$PATH" ;; esac
: "${ACCEPT_GH_LOGIN:?full.sh needs ACCEPT_GH_LOGIN, the test GitHub account}"
: "${ACCEPT_GO_PORT:?full.sh needs ACCEPT_GO_PORT, a small Go port that is due}"
: "${ACCEPT_RUST_PORT:?full.sh needs ACCEPT_RUST_PORT, a small Rust port that is due}"
: "${ACCEPT_STATE:=$HOME/.dockhand-acceptance/full}"
: "${MACPORTS_TREE:=$HOME/Source/macports-ports}"
mkdir -p "$ACCEPT_STATE"
ACCEPT_STATE=$(cd "$ACCEPT_STATE" && pwd -P)

# The remotes that are MacPorts' own, which H2 doesn't count as the fork.
upstream=""
if [ -d "$MACPORTS_TREE/.git" ]; then
	for remote in $(git -C "$MACPORTS_TREE" remote); do
		case "$(git -C "$MACPORTS_TREE" remote get-url "$remote")" in
		*github.com[:/]macports/macports-ports*) upstream="$upstream $remote" ;;
		esac
	done
fi

# Git reaches GitHub with the test account's key alone: the agent, and any
# key in ~/.ssh, may be a person's, whose pushes would be theirs.
: "${ACCEPT_GH_KEY:=$HOME/.dockhand-acceptance/test-account_ed25519}"
[ -r "$ACCEPT_GH_KEY" ] || { echo "full.sh: ACCEPT_GH_KEY, $ACCEPT_GH_KEY, isn't there; reset-user.sh puts the test account's key there" >&2; exit 2; }
export GIT_SSH_COMMAND="ssh -i $ACCEPT_GH_KEY -o IdentitiesOnly=yes -o IdentityAgent=none"

[ -d "$MACPORTS_TREE/.git" ] || { echo "full.sh: MACPORTS_TREE, $MACPORTS_TREE, isn't a clone; reset-user.sh clones the test account's fork there" >&2; exit 2; }

# Test pull requests go to the sandbox (engine.Options.PullRequests).
export DOCKHAND_PULL_REQUESTS=${ACCEPT_SANDBOX:-$ACCEPT_GH_LOGIN/macports-ports}
# dockhand's own configuration file, which rows that set something up
# for themselves (A10's maintainer, E10's command provider) change for
# their run and restore: the quick stage names a scratch one, and here it
# is dhtest's, where dockhand reads it by default (the rc1 full stage:
# A10 died in setup on DOCKHAND_CONFIG unset).
export DOCKHAND_CONFIG=${DOCKHAND_CONFIG:-$HOME/.dockhand/config.toml}
mkdir -p "$(dirname "$DOCKHAND_CONFIG")"
touch "$DOCKHAND_CONFIG"
export ACCEPT_STATE ACCEPT_GH_LOGIN ACCEPT_GO_PORT ACCEPT_RUST_PORT MACPORTS_TREE
export ACCEPT_REPO=$(cd "$here/../.." && pwd)
export DH_BIN=${DH_BIN:-/opt/local/bin/dockhand}
export ACCEPT_WATCH=$MACPORTS_TREE ACCEPT_UPSTREAM=${upstream# } ACCEPT_RUN_DIR=$MACPORTS_TREE
export ACCEPT_SECRET_DIRS="$HOME/.dockhand $HOME/Library/LaunchAgents"
# H9 watches a person's own state in the quick stage; here the state is
# the test's.
unset ACCEPT_HOME_DIRS ACCEPT_RESET DH_FAILPOINT_BIN
exec "$here/run.sh" --stage full "$@"
