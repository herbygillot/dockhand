# stages: quick full
# D-I6: serve killed with checks queued. The next serve resumes what was
# queued, and nothing is left running (H7).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	dh_json check -p "$(port)" -d || return 0
	DI6_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json")
	dh_bg serve --drain
	wait_for_line "$DH_BG_LOG" 'building in|leading' 900 || :
	kill -9 "$DH_BG_PID" 2>/dev/null
	dh_bg_wait || :
	dh serve --drain || :
	[ -n "$DI6_RUN" ] && dh_json wait "$DI6_RUN" || :
}
assert() {
	[ -n "${DI6_RUN:-}" ] || { row_fail "no check was queued"; return; }
	case "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" in
	0 | 2) row_pass "a second serve resumed $DI6_RUN to its end ($(jq -r '.result.run.state // empty' "$ROW_DIR/json/2.json"))" ;;
	*) row_fail "$DI6_RUN didn't finish after serve was killed: exit $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" ;;
	esac
}
