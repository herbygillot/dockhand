# stages: quick full
# A3: dockhand with no arguments, status, config, and --help on each
# command: nothing errors, and config shows each setting's source.

act() {
	dh
	dh status
	dh_json status
	dh config
	dh_json config
	dh --help
	for command in $(a3_commands) "setup tart" "setup github"; do
		# shellcheck disable=SC2086
		dh $command --help || printf 'help failed: %s\n' "$command" >>"$ROW_DIR/a3.failed"
	done
}

# a3_commands are the commands --help lists, by name.
a3_commands() {
	"$DH_BIN" --help | awk '/^Usage:/ {listing=1} listing && /^  [a-z][a-z-]*  +/ {print $1}'
}

assert() {
	if [ -s "$ROW_DIR/a3.failed" ]; then
		row_fail "$(tr '\n' ';' <"$ROW_DIR/a3.failed")"
		return
	fi
	local failed
	failed=$(grep -B1 '^\[exit [^0]' "$ROW_DIR/out.log" | grep '^\$ ' | head -3 | tr '\n' ';')
	if [ -n "$failed" ]; then
		row_fail "exited non-zero: $failed"
		return
	fi
	if ! jq -e '(.result.settings | length > 0) and all(.result.settings[]; .source != null and .source != "")' "$ROW_DIR/json/2.json" >/dev/null; then
		row_fail "config --json gives a setting without its source"
		return
	fi
	row_pass "every command's help, status, and config ran"
}
