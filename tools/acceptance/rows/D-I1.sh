# stages: quick full
# D-I1: Ctrl-C during update's fetch. The branch is left unchanged or with
# the edit whole, and running the update again finishes it.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	dh_bg -v update "$(port)" --new
	# The interrupt counts only where it reached the update still fetching
	# (#232).
	if wait_for_line "$DH_BG_LOG" '[Ff]etch|[Dd]ownload' 600; then
		sleep 1
		kill -INT "$DH_BG_PID" 2>/dev/null && DI1_INTERRUPTED=1
	fi
	# The background dockhand's own exit: "$?" after "|| :" was always 0
	# (Codex's cold read at 2cdaf571, #232).
	local exited=0
	dh_bg_wait || exited=$?
	echo "$exited" >"$ROW_DIR/di1.interrupted"
	dh_json update "$(port)" || :
}
assert() {
	local again=$ROW_DIR/json/1.json
	if [ -z "${DI1_INTERRUPTED:-}" ]; then
		row_result "not exercised" "the update was never interrupted mid-fetch: it reached no fetch, or had ended by the interrupt"
		return
	fi
	case "$(cat "$again.exit")" in
	0 | 3) row_pass "interrupted mid-fetch, and the update run again finished: $(jq -r '.result.after // .result.port // empty' "$again")" ;;
	*) row_fail "the update run again after the interrupt exited $(cat "$again.exit"): $(jq -r '.error // empty' "$again")" ;;
	esac
}
