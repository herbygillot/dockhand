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
	# Each result says what made it: the harness's commit and the binary
	# the row ran (#230).
	if ! jq -e '.harness_commit and .binary.path' "$state/results/selftest/$row.json" >/dev/null; then
		echo "selftest: $row's result doesn't record its harness and binary"
		fail=1
		return
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
for spec in ok:pass h1allowed:pass h2allowed:pass h1:blocker:H1 h2:blocker:H2 h2upstream:blocker:H2 h3:blocker:H3 h4:blocker:H4 h4present:pass h4added:blocker:H4 h5:blocker:H5 h6:blocker:H6 h6closed:pass h6cleaned:pass h6otherbranch:blocker:H6 h6wrapped:blocker:H6 h7:blocker:H7 h7teardown:blocker:H7 h7handoff:pass h7cut:not_run h7stopped:fail h8:blocker:H8 h9:blocker:H9 exercised:pass unexercised:not_exercised h2unread:blocked~_unobserved; do
	row=${spec%%:*}
	rest=${spec#*:}
	want=${rest%%:*}
	harm=""
	[ "$rest" = "$want" ] || harm=${rest#*:}
	want=${want//_/ }
	want=${want//\~/:}
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	mkdir -p "$tmp/state" "$tmp/fake" "$tmp/home/.dockhand"
	git init -q -b master "$tmp/clone"
	git -C "$tmp/clone" -c user.name=t -c user.email=t@example.org commit -q --allow-empty -m init
	git init -q --bare "$tmp/fork.git"
	git -C "$tmp/clone" remote add fork "$tmp/fork.git"
	git -C "$tmp/clone" push -q fork master
	git init -q --bare "$tmp/upstream.git"
	git -C "$tmp/clone" remote add upstream "$tmp/upstream.git"
	git -C "$tmp/clone" push -q upstream master
	(
		export ACCEPT_STATE="$tmp/state" ACCEPT_WATCH="$tmp/clone" ACCEPT_UPSTREAM="upstream" ACCEPT_RUN_DIR="$tmp/clone"
		export SELFTEST_CLONE="$tmp/clone" FAKE_DH_STATE="$tmp/fake" DH_BIN="$here/selftest/bin/dockhand"
		export DOCKHAND_DB="$tmp/state/db" DOCKHAND_CONFIG="$tmp/state/config.toml" MACPORTS_TREE="$tmp/state/clone"
		export DOCKHAND_UPSTREAM="$tmp/state/upstream.git" DOCKHAND_INDEX_CACHE="$tmp/state/index" DOCKHAND_READING_CACHE="$tmp/state/readings" DOCKHAND_GITHUB_CACHE="$tmp/state/github" DOCKHAND_ARCHIVE_CACHE="$tmp/state/archive-cache" DOCKHAND_BUILD_HISTORY_CACHE="$tmp/state/build-history"
		export DOCKHAND_TART_HOME="$tmp/state/tart/dockhand" DOCKHAND_SSH_DIR="$tmp/state/tart/ssh" TART_HOME="$tmp/state/tart/tart"
		export SELFTEST_HOME="$tmp/home" ACCEPT_HOME_DIRS="$tmp/home/.dockhand $tmp/home/.tart"
		export PATH="$here/selftest/bin:$PATH"
		unset ACCEPT_GH_LOGIN ACCEPT_SECRET_DIRS
		"$here/run.sh" --stage quick --candidate selftest --rows "$row" --row-dir "$here/selftest/rows" >/dev/null || :
	)
	check "$row" "$want" "$harm" "$tmp/state"
	rm -rf "$tmp"
done
# The host's preflight (#226): a host whose tart list hangs blocks a row
# before it runs, and a row that fails once the host turned bad during it
# is the host's, each on the rerun list, neither a fail.
host_case() {
	local row=$1 before=$2 want=$3 got
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	mkdir -p "$tmp/state/tart/dockhand" "$tmp/fake" "$tmp/home/.dockhand"
	git init -q -b master "$tmp/clone"
	git -C "$tmp/clone" -c user.name=t -c user.email=t@example.org commit -q --allow-empty -m init
	[ "$before" = hangs ] && : >"$tmp/fake/tart-hangs"
	(
		export ACCEPT_STATE="$tmp/state" ACCEPT_WATCH="$tmp/clone" ACCEPT_UPSTREAM="" ACCEPT_RUN_DIR="$tmp/clone" ACCEPT_TART_WAIT=2
		export SELFTEST_CLONE="$tmp/clone" FAKE_DH_STATE="$tmp/fake" DH_BIN="$here/selftest/bin/dockhand"
		export DOCKHAND_DB="$tmp/state/db" DOCKHAND_CONFIG="$tmp/state/config.toml" MACPORTS_TREE="$tmp/state/clone"
		export DOCKHAND_UPSTREAM="$tmp/state/upstream.git" DOCKHAND_INDEX_CACHE="$tmp/state/index" DOCKHAND_READING_CACHE="$tmp/state/readings" DOCKHAND_GITHUB_CACHE="$tmp/state/github" DOCKHAND_ARCHIVE_CACHE="$tmp/state/archive-cache" DOCKHAND_BUILD_HISTORY_CACHE="$tmp/state/build-history"
		export DOCKHAND_TART_HOME="$tmp/state/tart/dockhand" DOCKHAND_SSH_DIR="$tmp/state/tart/ssh" TART_HOME="$tmp/state/tart/tart"
		export SELFTEST_HOME="$tmp/home" ACCEPT_HOME_DIRS="$tmp/home/.dockhand"
		export PATH="$here/selftest/bin:$PATH"
		unset ACCEPT_GH_LOGIN ACCEPT_SECRET_DIRS
		"$here/run.sh" --stage quick --candidate selftest --rows "$row" --row-dir "$here/selftest/rows" >/dev/null || :
	)
	got=$(jq -r '.result + ": " + .why' "$tmp/state/results/selftest/$row.json" 2>/dev/null || echo "no result")
	if [[ "$got" == "$want"* ]] && grep -qx "$row" "$tmp/state/results/selftest/rerun.txt"; then
		echo "selftest: $row with the host's tart $before gave $got"
	else
		echo "selftest: $row with the host's tart $before gave $got, not $want..., or isn't on the rerun list"
		fail=1
	fi
	rm -rf "$tmp"
}
host_case ok hangs "blocked: host: tart list in "
host_case hostbad answering "blocked: host: the host turned bad during the row: tart list in "

# A7's and A8's candidate checks, which no unattended run reaches (#234):
# each passes when dockhand --version names the candidate, with or without
# its v and with no port revision asked, and fails on a newer build left
# in the overlay; A7's last checkpoint says to answer done when there's
# nothing to put back.
candidate_case() {
	local row=$1 version=$2 want=$3 got
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	mkdir -p "$tmp/row"
	printf '#!/bin/sh\necho "dockhand %s"\n' "$version" >"$tmp/dockhand"
	chmod +x "$tmp/dockhand"
	(
		export ACCEPT_STATE="$tmp" ACCEPT_STAGE=full ACCEPT_DRY=0 ROW_DIR="$tmp/row" ROW_ID=$row ROW_LIB="$here/lib"
		export ACCEPT_CANDIDATE=v0.8.0-rc4 DH_BIN="$tmp/dockhand" HOME="$tmp"
		# shellcheck source=lib/common.sh
		. "$here/lib/common.sh"
		# shellcheck source=lib/protocol.sh
		. "$here/lib/protocol.sh"
		say() { :; }
		# shellcheck disable=SC1090
		. "$here/rows/$row.sh"
		"$DH_BIN" --version >"$tmp/row/reinstalled"
		cp "$tmp/row/reinstalled" "$tmp/row/restored"
		cp "$tmp/row/reinstalled" "$tmp/row/upgraded"
		# A7's evidence of the upgrade itself, as a passing run leaves it.
		printf '$ dockhand status\n[exit 0]\n' >"$tmp/row/out.log"
		printf '2026-11-12T12:00:00Z serve: upgrade detected\n2026-11-12T12:00:01Z serve: stopped for the upgrade\n' >"$tmp/row/serve.log"
		echo 1 >"$tmp/row/pid.before"
		echo 2 >"$tmp/row/pid.after"
		: >"$tmp/row/agents" && : >"$tmp/row/agents.again"
		assert
	) >/dev/null 2>&1
	got=$(cat "$tmp/row/result" 2>/dev/null || echo "no result")
	if [ "$got" = "$want" ]; then
		echo "selftest: $row with --version \"dockhand $version\" is $got"
	else
		echo "selftest: $row with --version \"dockhand $version\" is $got, not $want: $(cat "$tmp/row/why" 2>/dev/null)"
		fail=1
	fi
	rm -rf "$tmp"
}
for row in A7 A8; do
	candidate_case "$row" v0.8.0-rc4 pass
	candidate_case "$row" 0.8.0-rc4 pass
	candidate_case "$row" 0.8.0-rc5 fail
done
if grep -q 'already names .*answer done' "$here/rows/A7.sh"; then
	echo "selftest: A7's last checkpoint says to answer done where the candidate is already installed"
else
	echo "selftest: A7's last checkpoint doesn't say what to answer where there's nothing to put back"
	fail=1
fi

# The guard: a quick run with dockhand's state outside the scratch
# directory, as your own database or Tart home, runs nothing.
guard_case() {
	local name=$1 value=$2 status=0
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	mkdir -p "$tmp/state"
	(
		export ACCEPT_STATE="$tmp/state" DOCKHAND_DB="$tmp/state/db" DOCKHAND_CONFIG="$tmp/state/config.toml"
		export MACPORTS_TREE="$tmp/state/clone" DOCKHAND_UPSTREAM="$tmp/state/upstream.git" DOCKHAND_INDEX_CACHE="$tmp/state/index" DOCKHAND_READING_CACHE="$tmp/state/readings" DOCKHAND_GITHUB_CACHE="$tmp/state/github" DOCKHAND_ARCHIVE_CACHE="$tmp/state/archive-cache" DOCKHAND_BUILD_HISTORY_CACHE="$tmp/state/build-history"
		export DOCKHAND_TART_HOME="$tmp/state/tart/dockhand" DOCKHAND_SSH_DIR="$tmp/state/tart/ssh" TART_HOME="$tmp/state/tart/tart"
		export "$name=$value"
		"$here/run.sh" --stage quick --candidate selftest --rows ok --row-dir "$here/selftest/rows" >/dev/null 2>"$tmp/guard.err"
	) || status=$?
	if [ "$status" -ne 2 ] || [ -e "$tmp/state/results" ] || ! grep -q "$name is $value, outside" "$tmp/guard.err"; then
		echo "selftest: the guard let a run with $name at $value through (exit $status): $(cat "$tmp/guard.err")"
		fail=1
	else
		echo "selftest: the guard refuses $name at $value"
	fi
	rm -rf "$tmp"
}
guard_case DOCKHAND_DB "$HOME/.dockhand/dockhand.db"
guard_case TART_HOME "$HOME/.tart"

# The full stage's protocol (lib/protocol.sh). A dry run walks every row
# only the full stage runs up to its first step for the host or a person,
# where each stops as not run, having run nothing of dockhand's.
tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
mkdir -p "$tmp/state" "$tmp/fake"
git init -q -b master "$tmp/clone"
git -C "$tmp/clone" -c user.name=t -c user.email=t@example.org commit -q --allow-empty -m init
fullrows=$(cd "$here/rows" && grep -lx '# stages: full' -- *.sh | sed 's/[.]sh$//' | tr '\n' ' ')
(
	export ACCEPT_STATE="$tmp/state" ACCEPT_WATCH="$tmp/clone" ACCEPT_UPSTREAM="" ACCEPT_RUN_DIR="$tmp/clone"
	export SELFTEST_CLONE="$tmp/clone" FAKE_DH_STATE="$tmp/fake" DH_BIN="$here/selftest/bin/dockhand"
	export DOCKHAND_DB="$tmp/state/db" DOCKHAND_CONFIG="$tmp/state/config.toml" MACPORTS_TREE="$tmp/state/clone"
	export DOCKHAND_UPSTREAM="$tmp/state/upstream.git" DOCKHAND_INDEX_CACHE="$tmp/state/index" DOCKHAND_READING_CACHE="$tmp/state/readings" DOCKHAND_GITHUB_CACHE="$tmp/state/github" DOCKHAND_ARCHIVE_CACHE="$tmp/state/archive-cache" DOCKHAND_BUILD_HISTORY_CACHE="$tmp/state/build-history"
	export DOCKHAND_TART_HOME="$tmp/state/tart/dockhand" DOCKHAND_SSH_DIR="$tmp/state/tart/ssh" TART_HOME="$tmp/state/tart/tart"
	export ACCEPT_GO_PORT=go-port ACCEPT_RUST_PORT=rust-port PATH="$here/selftest/bin:$PATH"
	unset ACCEPT_GH_LOGIN ACCEPT_SECRET_DIRS ACCEPT_HOME_DIRS
	# shellcheck disable=SC2086
	"$here/run.sh" --stage full --dry-run --candidate dry --rows "$fullrows" >/dev/null || :
)
walked=0
for row in $fullrows; do
	result=$(jq -r '.result + ": " + .why' "$tmp/state/results/dry/$row.json" 2>/dev/null || echo "no result")
	case "$result" in
	"not run: host only, from: "* | "not run: waits on a person, from: "*) walked=$((walked + 1)) ;;
	*) echo "selftest: the dry run of $row gave $result"; fail=1 ;;
	esac
done
ran=$(grep -l '^\$ dockhand' "$tmp"/state/results/dry/*/out.log 2>/dev/null | head -3 | tr '\n' ' ' || :)
[ -z "$ran" ] || { echo "selftest: a dry run ran dockhand in $ran"; fail=1; }
echo "selftest: a dry run walked $walked full-stage rows, each to its first step for the host or a person"
rm -rf "$tmp"

# A checkpoint waits on WAITING until resume.sh answers: done goes on,
# and fail fails the row.
for answer in done fail; do
	tmp=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	mkdir -p "$tmp/row"
	(
		export ACCEPT_STATE="$tmp" ACCEPT_STAGE=full ACCEPT_DRY=0 ROW_DIR="$tmp/row" ROW_ID=X1
		# shellcheck source=lib/common.sh
		. "$here/lib/common.sh"
		# shellcheck source=lib/protocol.sh
		. "$here/lib/protocol.sh"
		say() { :; }
		if checkpoint "do the thing"; then echo went-on >"$tmp/row/after"; fi
	) &
	waiter=$!
	for _ in 1 2 3 4 5 6 7 8 9 10; do [ -f "$tmp/waiting" ] && break; sleep 1; done
	ACCEPT_STATE="$tmp" "$here/resume.sh" "$answer"
	wait "$waiter" || :
	case "$answer:$(cat "$tmp/row/after" 2>/dev/null):$(cat "$tmp/row/result" 2>/dev/null)" in
	done:went-on: | fail::fail) echo "selftest: a checkpoint answered $answer did as it should" ;;
	*) echo "selftest: a checkpoint answered $answer: after=$(cat "$tmp/row/after" 2>/dev/null) result=$(cat "$tmp/row/result" 2>/dev/null)"; fail=1 ;;
	esac
	rm -rf "$tmp"
done
# with_timeout ends a command that ignores the alarm and TERM, as Go does
# SIGALRM, by KILL after its grace, and keeps a command's own exit.
(
	ROW_DIR=$(mktemp -d "${TMPDIR:-/tmp}/dockhand-selftest.XXXXXX")
	# shellcheck source=lib/common.sh
	. "$here/lib/common.sh"
	started=$SECONDS
	status=0
	with_timeout 2 sh -c 'trap "" ALRM TERM; sleep 30' || status=$?
	took=$((SECONDS - started))
	own=0
	with_timeout 5 sh -c 'exit 7' || own=$?
	rm -rf "$ROW_DIR"
	if [ "$status" = 142 ] && [ "$took" -lt 15 ] && [ "$own" = 7 ]; then
		echo "selftest: with_timeout ends a command that ignores its alarm, in ${took}s, and keeps a command's own exit"
	else
		echo "selftest: with_timeout gave $status after ${took}s, and $own for a command's own exit 7"
		exit 1
	fi
) || fail=1
# No row takes the first dockhand/* branch for its own: alphabetical, and
# archived branches keep their refs, so it was another row's (the rc6 full
# stage: D-S4 put its hand commit on b10-broken). own_branch is the row's.
if picked=$(grep -nE "branch --list 'dockhand/\*'.*\| *head" "$here"/rows/*.sh); then
	echo "selftest: rows take the first dockhand/* branch rather than own_branch:"
	printf '%s\n' "$picked"
	fail=1
else
	echo "selftest: no row takes the first dockhand/* branch for its own"
fi
# A serve line is matched through SERVE_LINE, which allows the time serve
# starts each line with where its output isn't a terminal (0a3ea46e): rows
# anchored on "^serve: " missed every line of serve.log in rc4.
if anchored=$(grep -nE "\^serve:" "$here"/rows/*.sh); then
	echo "selftest: rows anchor serve's lines on ^serve: rather than SERVE_LINE:"
	printf '%s\n' "$anchored"
	fail=1
else
	echo "selftest: rows match serve's lines through SERVE_LINE"
fi
exit "$fail"
