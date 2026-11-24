#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
lock=$tmp/lock
wrapper=
child=
cleanup() {
	if [ -f "$tmp/cmd.pid" ]; then
		child=$(cat "$tmp/cmd.pid")
	fi
	if [ -n "$child" ]; then
		kill -TERM "$child" 2>/dev/null || :
	fi
	if [ -n "$wrapper" ]; then
		kill -TERM "$wrapper" 2>/dev/null || :
	fi
	if [ -n "$wrapper" ]; then
		wait "$wrapper" 2>/dev/null || :
	fi
	rm -rf "$tmp"
}
trap cleanup EXIT HUP INT TERM
DOCKHAND_HEAVY_LOCK=$lock
DOCKHAND_HEAVY_TEST_TMP=$tmp
export DOCKHAND_HEAVY_LOCK DOCKHAND_HEAVY_TEST_TMP

wait_for_file() {
	file=$1
	i=0
	while [ ! -s "$file" ]; do
		i=$((i + 1))
		if [ "$i" -ge 200 ]; then
			return 1
		fi
		sleep 0.01
	done
}

wait_for_record() {
	wait_for_file "$lock"
	grep -E '\(pid [0-9]+,' "$lock" >/dev/null
}

test_release_clears_holder() {
	"$repo/tools/heavy" sh -c 'exit 0' >"$tmp/out" 2>"$tmp/err"
	if [ -s "$lock" ]; then
		echo "released lock still names a holder: $(cat "$lock")" >&2
		exit 1
	fi
}

test_dead_holder_is_reported_and_replaced() {
	stale_pid=$(sh -c 'echo $$')
	printf 'old command (pid %s, in /tmp, since yesterday)\n' "$stale_pid" >"$lock"
	"$repo/tools/heavy" sh -c 'exit 0' >"$tmp/out" 2>"$tmp/err"
	if ! grep -F "heavy: holder $stale_pid is gone" "$tmp/err" >/dev/null; then
		echo "stale lock holder was not reported gone: $(cat "$tmp/err")" >&2
		exit 1
	fi
	if [ -s "$lock" ]; then
		echo "stale lock was not cleared after command exit: $(cat "$lock")" >&2
		exit 1
	fi
}

test_killing_recorded_pid_stops_command_and_clears_record() {
	rm -f "$tmp/cmd.pid"
	"$repo/tools/heavy" perl -e 'open my $f, ">", "$ENV{DOCKHAND_HEAVY_TEST_TMP}/cmd.pid" or die $!; print $f "$$\n"; close $f; sleep 30' >"$tmp/out" 2>"$tmp/err" &
	wrapper=$!
	wait_for_file "$tmp/cmd.pid"
	wait_for_record
	recorded_pid=$(sed -n 's/.*(pid \([0-9][0-9]*\),.*/\1/p' "$lock")
	child=$(cat "$tmp/cmd.pid")
	if [ "$recorded_pid" != "$child" ]; then
		echo "lock names pid $recorded_pid, command is $child" >&2
		exit 1
	fi
	kill -TERM "$recorded_pid"
	status=0
	wait "$wrapper" || status=$?
	wrapper=
	if [ "$status" -ne 143 ] || [ -s "$lock" ]; then
		echo "killing recorded pid returned $status and left lock: $(cat "$lock")" >&2
		exit 1
	fi
	child=
}

test_term_to_wrapper_reaches_command_and_clears_record() {
	rm -f "$tmp/cmd.pid" "$tmp/term-received"
	"$repo/tools/heavy" perl -e '$SIG{TERM} = sub { open my $f, ">", "$ENV{DOCKHAND_HEAVY_TEST_TMP}/term-received" or die $!; close $f; exit 143 }; open my $f, ">", "$ENV{DOCKHAND_HEAVY_TEST_TMP}/cmd.pid" or die $!; print $f "$$\n"; close $f; sleep 30' >"$tmp/out" 2>"$tmp/err" &
	wrapper=$!
	wait_for_file "$tmp/cmd.pid"
	wait_for_record
	child=$(cat "$tmp/cmd.pid")
	kill -TERM "$wrapper"
	status=0
	wait "$wrapper" || status=$?
	wrapper=
	if [ ! -f "$tmp/term-received" ] || [ "$status" -ne 143 ] || [ -s "$lock" ]; then
		echo "wrapper SIGTERM returned $status, reached command=$([ -f "$tmp/term-received" ] && echo yes || echo no), lock=$(cat "$lock")" >&2
		exit 1
	fi
	child=
}

test_normal_exit_status_is_preserved() {
	status=0
	"$repo/tools/heavy" perl -e 'exit 3' >"$tmp/out" 2>"$tmp/err" || status=$?
	if [ "$status" -ne 3 ]; then
		echo "heavy returned $status for command exit 3" >&2
		exit 1
	fi
}

test_nohup_keeps_sighup_ignored_for_command() {
	nohup "$repo/tools/heavy" perl -e 'exit($SIG{HUP} eq "IGNORE" ? 0 : 1)' >"$tmp/out" 2>"$tmp/err"
}

test_release_clears_holder
test_dead_holder_is_reported_and_replaced
test_killing_recorded_pid_stops_command_and_clears_record
test_term_to_wrapper_reaches_command_and_clears_record
test_normal_exit_status_is_preserved
test_nohup_keeps_sighup_ignored_for_command
