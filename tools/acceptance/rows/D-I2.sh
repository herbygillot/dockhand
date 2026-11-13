# stages: quick full
# D-I2: kill -9 during a Tart check. status says it stopped; wait resumes
# it; clean leaves no clone behind (H7).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup update "$(port)" --new || return 0
	# At the default level, as an agent reads it: off a terminal a
	# check says each step (34fa8f65), where rc5 needed -v (the docs
	# dig's I29).
	# --fresh, so the check builds and the kill meets a build: rc4's
	# check-1019 reused results and ended before the kill.
	dh_bg check -p "$(port)" --fresh
	# The kill counts only where it met the check building: a wait that
	# timed out, or a check already ended, tested nothing (Codex's cold
	# read at 2cdaf571, #232).
	if wait_for_line "$DH_BG_LOG" 'building in' 900 && kill -9 "$DH_BG_PID" 2>/dev/null; then
		DI2_KILLED=1
	fi
	dh_bg_wait || :
	DI2_RUN=$(grep -oE 'check-[0-9]+' "$DH_BG_LOG" | head -1)
	dh_json status --all || :
	[ -n "$DI2_RUN" ] && dh_json wait "$DI2_RUN" || :
	dh clean --yes || :
}
assert() {
	if [ -z "${DI2_RUN:-}" ] || [ -z "${DI2_KILLED:-}" ]; then
		row_result "not exercised" "the check was never killed while building${DI2_RUN:+: $DI2_RUN ran without it}"
		return
	fi
	# The run's own state in status, not the word: "stopped": 0 in the
	# envelope matched before (rc4's evidence read).
	if ! jq -e --arg run "$DI2_RUN" '.result.queue[]? | select(.name == $run and .stopped == true)' "$ROW_DIR/json/1.json" >/dev/null; then
		row_fail "status didn't say $DI2_RUN stopped: $(jq -c --arg run "$DI2_RUN" '[.result.queue[]? | select(.name == $run) | {state, stopped}]' "$ROW_DIR/json/1.json" 2>/dev/null)"
		return
	fi
	# The killed attempt's staged tree goes with the check's end, where it
	# stayed beside its log (Prime-time's v0.7.0 acceptance, 173 MB).
	local staged
	staged=$(find "${DOCKHAND_DB%/*}/logs/$DI2_RUN" -name input.tar 2>/dev/null | head -3 | tr '\n' ' ')
	if [ -n "$staged" ]; then
		row_fail "$DI2_RUN left its staged tree after wait: $staged"
		return
	fi
	case "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" in
	0 | 2) row_pass "status said $DI2_RUN stopped; wait resumed it to its end ($(jq -r '.result.run.state // empty' "$ROW_DIR/json/2.json")), leaving no staged tree" ;;
	*) row_fail "wait on the killed $DI2_RUN exited $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/2.json")" ;;
	esac
}
