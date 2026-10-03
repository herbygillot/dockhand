#!/usr/bin/env bash
# selftest.sh proves the runner and the harm sweep: a harmless row passes,
# one the row says it changes passes, and each invariant catches a row
# made to break it (the project's plan/acceptance-harness.md, H1). It runs
# against a stand-in dockhand, gh, and tart, in a scratch directory.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
fail=0
check() {
	local row=$1 want=$2 harm=${3:-} state=$4 got verdict
	got=$(jq -r .result "$state/results/selftest/$row.json")
	if [ "$got" != "$want" ]; then
		echo "selftest: $row is $got, not $want: $(jq -r .why "$state/results/selftest/$row.json")"
		fail=1
		return
	fi
	if [ -n "$harm" ]; then
		verdict=$(jq -r ".harm.$harm" "$state/results/selftest/$row.json")
		case "$verdict" in broken*) ;; *)
			echo "selftest: $row's $harm is $verdict, not broken"
			fail=1
			;;
		esac
	fi
	# Only the invariant the row breaks is broken.
	local others
	others=$(jq -r --arg h "$harm" '.harm | to_entries[] | select(.key != $h and (.value | startswith("broken"))) | .key + ": " + .value' "$state/results/selftest/$row.json")
	if [ -n "$others" ]; then
		echo "selftest: $row also broke $others"
		fail=1
		return
	fi
	echo "selftest: $row $got${harm:+ ($harm)}: $(jq -r '[.harm | to_entries[] | .key + " " + (.value | split(":")[0])] | join(", ")' "$state/results/selftest/$row.json")"
}
for spec in ok:pass h1allowed:pass h2allowed:pass h1:blocker:H1 h2:blocker:H2 h3:blocker:H3 h4:blocker:H4 h5:blocker:H5 h6:blocker:H6 h7:blocker:H7 h8:blocker:H8; do
	row=${spec%%:*}
	rest=${spec#*:}
	want=${rest%%:*}
	harm=""
	[ "$rest" = "$want" ] || harm=${rest#*:}
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	mkdir -p "$tmp/state" "$tmp/fake"
	git init -q -b master "$tmp/clone"
	git -C "$tmp/clone" -c user.name=t -c user.email=t@example.org commit -q --allow-empty -m init
	git init -q --bare "$tmp/fork.git"
	git -C "$tmp/clone" remote add fork "$tmp/fork.git"
	git -C "$tmp/clone" push -q fork master
	(
		export ACCEPT_STATE="$tmp/state" ACCEPT_WATCH="$tmp/clone" ACCEPT_UPSTREAM="" ACCEPT_RUN_DIR="$tmp/clone"
		export SELFTEST_CLONE="$tmp/clone" FAKE_DH_STATE="$tmp/fake" DH_BIN="$here/selftest/bin/dockhand"
		export DOCKHAND_DB="$tmp/state/db" DOCKHAND_CONFIG="$tmp/state/config.toml" MACPORTS_TREE="$tmp/state/clone"
		export DOCKHAND_UPSTREAM="$tmp/state/upstream.git" DOCKHAND_INDEX_CACHE="$tmp/state/index" DOCKHAND_READING_CACHE="$tmp/state/readings"
		export PATH="$here/selftest/bin:$PATH"
		unset ACCEPT_GH_LOGIN ACCEPT_SECRET_DIRS
		"$here/run.sh" --stage quick --candidate selftest --rows "$row" --row-dir "$here/selftest/rows" >/dev/null || :
	)
	check "$row" "$want" "$harm" "$tmp/state"
	rm -rf "$tmp"
done
# The guard: a quick run with dockhand's state outside the scratch
# directory, as your own database, runs nothing.
tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
mkdir -p "$tmp/state"
status=0
(
	export ACCEPT_STATE="$tmp/state" DOCKHAND_DB="$HOME/.dockhand/dockhand.db" DOCKHAND_CONFIG="$tmp/state/config.toml"
	export MACPORTS_TREE="$tmp/state/clone" DOCKHAND_UPSTREAM="$tmp/state/upstream.git" DOCKHAND_INDEX_CACHE="$tmp/state/index" DOCKHAND_READING_CACHE="$tmp/state/readings"
	"$here/run.sh" --stage quick --candidate selftest --rows ok --row-dir "$here/selftest/rows" >/dev/null 2>"$tmp/guard.err"
) || status=$?
if [ "$status" -ne 2 ] || [ -e "$tmp/state/results" ] || ! grep -q "DOCKHAND_DB is $HOME/.dockhand/dockhand.db, outside" "$tmp/guard.err"; then
	echo "selftest: the guard let a run near your own database through (exit $status): $(cat "$tmp/guard.err")"
	fail=1
else
	echo "selftest: the guard refuses your own database"
fi
rm -rf "$tmp"
exit "$fail"
