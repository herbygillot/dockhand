# stages: quick full
# D-I1: Ctrl-C during update's fetch. The branch is left unchanged or with
# the edit whole, and running the update again finishes it.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	dh_bg -v update "$(port)" --new
	if wait_for_line "$DH_BG_LOG" '[Ff]etch|[Dd]ownload' 600; then
		sleep 1
		kill -INT "$DH_BG_PID" 2>/dev/null
	fi
	dh_bg_wait || :
	echo "$?" >"$ROW_DIR/di1.interrupted"
	dh_json update "$(port)" || :
}
assert() {
	local again=$ROW_DIR/json/1.json
	if ! grep -qE '[Ff]etch|[Dd]ownload' "$ROW_DIR/out.log"; then
		row_known "the update never reached a fetch to interrupt"
		return
	fi
	case "$(cat "$again.exit")" in
	0 | 3) row_pass "interrupted mid-fetch, and the update run again finished: $(jq -r '.result.after // .result.port // empty' "$again")" ;;
	*) row_fail "the update run again after the interrupt exited $(cat "$again.exit"): $(jq -r '.error // empty' "$again")" ;;
	esac
}
