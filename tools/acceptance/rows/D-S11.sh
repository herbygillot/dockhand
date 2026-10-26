# stages: full
# D-S11: a check whose build stops writing says so, at the default level,
# as an agent reads it (the docs dig's I29; v0.6.0's 34fa8f65): a command
# provider stub writes a line, then nothing for eleven minutes, and the
# foreground check, off a terminal and without -v, prints each step and
# the stalled-log line, "<log>'s log hasn't grown in 10 minutes", before its
# result.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	cat >"$ROW_DIR/provider.sh" <<'SH'
#!/bin/sh
# A stand-in provider whose build goes quiet, as arowpp's bazel did: a
# line, then eleven minutes of nothing, then every target passed.
request=$1
result=$(jq -r .result "$request")
echo "building, then going quiet"
sleep 660
jq '{version: 1, targets: [.targets[] | {id, outcome: "passed", tests: "none"}]}' "$request" >"$result"
SH
	chmod +x "$ROW_DIR/provider.sh"
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/config.before"
	cat >>"$DOCKHAND_CONFIG" <<TOML

[providers.command]
run = "$ROW_DIR/provider.sh"
name = "acceptance quiet stub"
TOML
}
act() {
	host_only "a check whose build goes quiet for eleven minutes" || return 0
	dh_setup update "$(port)" --new || return 0
	# Off a terminal, at the default level: no -v.
	dh check -p "$(port)" --on command </dev/null || :
}
assert() {
	grep -q "^\$ dockhand check -p" "$ROW_DIR/out.log" || return 0
	if ! grep -qE "log hasn't grown in 10 minutes" "$ROW_DIR/out.log"; then
		row_fail "the check's quiet build was never said, at the default level: $(grep -E '⋯' "$ROW_DIR/out.log" | tail -2 | tr '\n' ' ')"
		return
	fi
	if ! grep -qE '^  ⋯ ' "$ROW_DIR/out.log"; then
		row_fail "the check printed no step off a terminal at the default level"
		return
	fi
	row_pass "off a terminal, without -v, the check said each step and that its log hadn't grown in 10 minutes"
}
teardown() {
	[ -f "$ROW_DIR/config.before" ] && cp "$ROW_DIR/config.before" "$DOCKHAND_CONFIG"
	return 0
}
