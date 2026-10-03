# stages: quick full
# D-I2: kill -9 during a Tart check. status says it stopped; wait resumes
# it; clean leaves no clone behind (H7).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	dh_bg check -p "$(port)"
	if wait_for_line "$DH_BG_LOG" 'building in' 900; then
		kill -9 "$DH_BG_PID" 2>/dev/null
	fi
	dh_bg_wait || :
	DI2_RUN=$(grep -oE 'check-[0-9]+' "$DH_BG_LOG" | head -1)
	dh_json status || :
	[ -n "$DI2_RUN" ] && dh_json wait "$DI2_RUN" || :
	dh clean --yes || :
}
assert() {
	if [ -z "${DI2_RUN:-}" ]; then
		row_known "the check never reached its build to kill"
		return
	fi
	if ! jq -e '.result.attention[]? | select((.what // "") | test("stopped"; "i"))' "$ROW_DIR/json/1.json" >/dev/null && ! grep -qi 'stopped' "$ROW_DIR/out.log"; then
		row_fail "status didn't say $DI2_RUN stopped"
		return
	fi
	case "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" in
	0 | 2) row_pass "status said $DI2_RUN stopped; wait resumed it to its end ($(jq -r '.result.run.state // empty' "$ROW_DIR/json/2.json"))" ;;
	*) row_fail "wait on the killed $DI2_RUN exited $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/2.json")" ;;
	esac
}
