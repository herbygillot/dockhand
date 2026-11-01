# stages: quick full
# F12: dockhand console --plain, the console as a script, SSH, or a screen
# reader reads it: status once, then each event as a line as it's
# journaled, a branch archived meanwhile among them (a start is a verbose
# event, which the console leaves out, as status --watch did), until
# it's stopped, which is its ordinary end. status --watch, which it
# replaces, refuses, naming console.
setup() {
	F12_BRANCH=$(run_name f12)
	dh_setup start "$F12_BRANCH" </dev/null || return 1
}
act() {
	dh status --watch </dev/null || :
	dh_bg console --plain
	if wait_for_line "$DH_BG_LOG" 'Watching for changes' 120; then
		dh archive "$F12_BRANCH" </dev/null || :
		wait_for_line "$DH_BG_LOG" "$F12_BRANCH archived" 60 || :
	fi
	kill -INT "$DH_BG_PID" 2>/dev/null
	F12_EXIT=0
	dh_bg_wait || F12_EXIT=$?
}
assert() {
	if ! grep -A3 -F '$ dockhand status --watch' "$ROW_DIR/out.log" | matches 'status --watch is now dockhand console'; then
		row_fail "status --watch didn't refuse naming console: $(grep -A2 -F '$ dockhand status --watch' "$ROW_DIR/out.log" | tail -2 | tr '\n' ' ')"
		return
	fi
	if ! matches 'Watching for changes' "$DH_BG_LOG"; then
		row_fail "console --plain never said it was watching: $(head -3 "$DH_BG_LOG" | tr '\n' ' ')"
		return
	fi
	if ! matches -F "$F12_BRANCH archived" "$DH_BG_LOG"; then
		row_fail "console --plain didn't print $F12_BRANCH's archiving as an event"
		return
	fi
	if LC_ALL=C grep -q "$(printf '\033')" "$DH_BG_LOG"; then
		row_fail "escape codes in console --plain's output"
		return
	fi
	if [ "${F12_EXIT:-1}" != 0 ]; then
		row_fail "console --plain, interrupted, exited $F12_EXIT, not 0"
		return
	fi
	row_pass "status --watch refused naming console; console --plain printed status, then $(grep -F "$F12_BRANCH archived" "$DH_BG_LOG" | head -1), and ended at Ctrl-C, exit 0"
}
