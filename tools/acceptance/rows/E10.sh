# stages: quick full
# E10: the command provider: a stub script that writes a result file runs
# a check, and its result reads back.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	cat >"$ROW_DIR/provider.sh" <<'SH'
#!/bin/sh
# A stand-in provider (docs/command-provider.md): every target it's asked
# for passes, written to the result file the request names.
request=$1
result=$(jq -r .result "$request")
jq '{version: 1, targets: [.targets[] | {id, outcome: "passed", tests: "none"}]}' "$request" >"$result"
SH
	chmod +x "$ROW_DIR/provider.sh"
	cat >>"${DOCKHAND_CONFIG:?}" <<TOML

[providers.command]
run = "$ROW_DIR/provider.sh"
name = "acceptance stub"
TOML
}
act() {
	dh_setup update "$(port)" --new || return 0
	dh_json check -p "$(port)" --on command || :
}
assert() {
	case "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" in
	0) row_pass "the command provider ran the check, and its result read back: $(jq -r '.result.run.state // empty' "$ROW_DIR/json/1.json")" ;;
	*) row_fail "check --on command exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/1.json")" ;;
	esac
}
