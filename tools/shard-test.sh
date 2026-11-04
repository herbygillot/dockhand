#!/bin/sh
# shard-test.sh PACKAGE SHARDS TIMEOUT [go test build flags...]
#
# Runs one package's tests as SHARDS processes of its test binary, each
# with its own share of the tests, so a package whose tests can't run in
# parallel within one process still uses the Mac's cores. The command
# line's tests set package-level seams and HOME, which each process keeps
# to itself (the test suite analysis of 2026-10-02). A shard's output is
# shown when it fails; the line at the end reads as go test's.
set -eu
package=$1 shards=$2 timeout=$3
shift 3
go=${GO:-go}
directory=$("$go" list -f '{{.Dir}}' "$package")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT INT TERM
"$go" test -c -o "$work/package.test" "$@" "$package"
cd "$directory"
# A binary that can't list its tests, as one whose init panics, or that
# lists none fails the run with what it said: `|| true` read either as ok
# (Codex's sixth review, #75).
if ! "$work/package.test" -test.list '.*' >"$work/listed" 2>&1; then
	cat "$work/listed"
	printf 'FAIL\t%s\t(listing its tests failed)\n' "$package"
	exit 1
fi
grep -E '^(Test|Example|Fuzz)' "$work/listed" >"$work/names" || true
if [ ! -s "$work/names" ]; then
	cat "$work/listed"
	printf 'FAIL\t%s\t(it lists no tests)\n' "$package"
	exit 1
fi
start=$(date +%s)
shard=0
while [ "$shard" -lt "$shards" ]; do
	names=$(awk -v n="$shards" -v i="$shard" 'NR % n == i' "$work/names" | paste -sd '|' -)
	if [ -n "$names" ]; then
		("$work/package.test" -test.timeout "$timeout" -test.run "^($names)\$" >"$work/out.$shard" 2>&1 && echo 0 || echo 1) >"$work/status.$shard" &
	fi
	shard=$((shard + 1))
done
wait
failed=0
for status in "$work"/status.*; do
	[ -e "$status" ] || continue
	if [ "$(cat "$status")" != 0 ]; then
		failed=1
		cat "$work/out.${status##*.}"
	fi
done
elapsed=$(($(date +%s) - start))
if [ "$failed" = 0 ]; then
	printf 'ok  \t%s\t%ss (%s shards)\n' "$package" "$elapsed" "$shards"
else
	printf 'FAIL\t%s\t%ss (%s shards)\n' "$package" "$elapsed" "$shards"
	exit 1
fi
