#!/usr/bin/env bash
# quick.sh runs the quick stage (the project's plan/prime-time.md): it
# builds the dockhand under test from this checkout, sets up a fresh
# scratch environment, and runs the quick stage's rows there.
#
#   tools/acceptance/quick.sh [--candidate <rc>] [--rows "A3 B1"]
#
# The environment lives in ACCEPT_STATE, ~/.dockhand-acceptance/quick
# unless set:
#   - its own database, configuration, caches, and worktrees;
#   - a scratch ports clone whose master, upstream.git, is pinned at
#     ACCEPT_PIN, a fixed older commit, so the same ports are always due
#     and two runs compare;
#   - a local bare fork, fork.git, so nothing can be pushed anywhere real.
# The pinned upstream borrows the objects of ACCEPT_PORTS_SOURCE, your
# ports clone, which is only read. Everything but upstream.git and the
# results is made afresh each run. Nothing runs against your own
# database or configuration: lib/guard.sh refuses that.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
: "${ACCEPT_STATE:=$HOME/.dockhand-acceptance/quick}"
: "${ACCEPT_PORTS_SOURCE:=$HOME/Source/macports-ports}"
: "${ACCEPT_PIN:=026b3878a258b85619633709d89871f2f8750b94}"
mkdir -p "$ACCEPT_STATE"
state=$(cd "$ACCEPT_STATE" && pwd -P)
export ACCEPT_STATE=$state

source_objects=$(git -C "$ACCEPT_PORTS_SOURCE" rev-parse --path-format=absolute --git-common-dir)/objects
if ! git -C "$ACCEPT_PORTS_SOURCE" cat-file -e "$ACCEPT_PIN^{commit}" 2>/dev/null; then
	echo "quick.sh: $ACCEPT_PORTS_SOURCE hasn't got $ACCEPT_PIN; fetch MacPorts' master there, or set ACCEPT_PIN" >&2
	exit 2
fi

# The pinned upstream, kept between runs: an empty bare repository that
# borrows the ports clone's objects, its master at the pin.
if [ ! -d "$state/upstream.git" ]; then
	git init -q --bare -b master "$state/upstream.git"
	printf '%s\n' "$source_objects" >"$state/upstream.git/objects/info/alternates"
fi
git -C "$state/upstream.git" update-ref refs/heads/master "$ACCEPT_PIN"

# The rest afresh: the fork, the clone, dockhand's own state.
rm -rf "$state/fork.git" "$state/clone" "$state/home" "$state/worktrees" "$state/bin"
git init -q --bare -b master "$state/fork.git"
printf '%s\n' "$source_objects" >"$state/fork.git/objects/info/alternates"
git clone -q --shared --no-checkout "$state/upstream.git" "$state/clone"
git -C "$state/clone" sparse-checkout set --cone _resources
git -C "$state/clone" checkout -q master
git -C "$state/clone" remote add fork "$state/fork.git"
git -C "$state/clone" config user.name "Dockhand Acceptance"
git -C "$state/clone" config user.email acceptance@example.invalid
mkdir -p "$state/home" "$state/worktrees" "$state/bin"
cat >"$state/home/config.toml" <<TOML
worktrees = "$state/worktrees"

[cleanup]
automatic = false
TOML

# The dockhand under test, built from this checkout.
if [ -z "${DH_BIN:-}" ]; then
	(cd "$repo" && make -s build BINARY="$state/bin/dockhand")
	DH_BIN="$state/bin/dockhand"
fi

export DH_BIN
export DOCKHAND_DB="$state/home/dockhand.db" DOCKHAND_CONFIG="$state/home/config.toml"
export MACPORTS_TREE="$state/clone" DOCKHAND_UPSTREAM="$state/upstream.git"
export DOCKHAND_INDEX_CACHE="$state/home/index" DOCKHAND_READING_CACHE="$state/home/readings"
export ACCEPT_WATCH="$state/clone" ACCEPT_UPSTREAM=origin ACCEPT_RUN_DIR="$state/clone"
export ACCEPT_SECRET_DIRS="$state/home"
unset ACCEPT_GH_LOGIN
"$DH_BIN" setup -y >"$state/home/setup.log" 2>&1 || {
	echo "quick.sh: dockhand setup failed in the scratch environment:" >&2
	cat "$state/home/setup.log" >&2
	exit 1
}
exec "$here/run.sh" --stage quick "$@"
