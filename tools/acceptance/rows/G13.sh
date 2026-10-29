# stages: full
# G13: a batch's checks run together (v0.7.0). With no serve leading,
# ship of three small outdated ports, --yes --to check --on tart, queues
# the three checks, then runs them together within Tart's two VMs, where
# they ran one at a time in input order: a second check starts before the
# first records a result, and each change is finished as its check ends.
# ACCEPT_BATCH_PORTS names the three, the day's, since only a person
# knows which are outdated and small; without it the row isn't run. It
# stops at the checks, opening no pull request; the submissions' order is
# the command's tests'.
ports() { printf '%s' "${ACCEPT_BATCH_PORTS:-}"; }

act() {
	host_only "three small outdated ports, checked in Tart with no serve" || return 0
	# shellcheck disable=SC2046
	set -- $(ports)
	if [ $# -ne 3 ]; then
		row_result "not run" "ACCEPT_BATCH_PORTS names $# ports, not three small outdated ones of the day"
		return 0
	fi
	if "$DH_BIN" --json status --all 2>/dev/null | jq -e '.result.serve_state.running == true' >/dev/null; then
		row_result "not run" "a serve is running, which would run the checks itself"
		return 0
	fi
	G13_STARTED=$(date +%s)
	dh ship "$@" --yes --to check --on tart -v || :
	G13_TOOK=$(($(date +%s) - G13_STARTED))
	sed -n '/^\$ dockhand ship /,/^\[exit/p' "$ROW_DIR/out.log" >"$ROW_DIR/ship.log"
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/ship.log" ] || return 0
	local exit started first
	exit=$(sed -n 's/^\[exit \([0-9]*\)\]$/\1/p' "$ROW_DIR/ship.log" | tail -1)
	case "$exit" in
	0 | 3) ;;
	*) row_fail "ship exited $exit: $(grep -v '^\[exit' "$ROW_DIR/ship.log" | tail -2 | tr '\n' ' ')"; return ;;
	esac
	started=$(grep -cE 'check-[0-9]+: attempt 1 of 3 on ' "$ROW_DIR/ship.log")
	[ "$started" -ge 3 ] || { row_fail "ship started $started checks, not three: $(grep -E 'attempt 1 of 3' "$ROW_DIR/ship.log" | tr '\n' ';')"; return; }
	# How many had started before the first result was recorded.
	first=$(grep -nE ': [A-Za-z0-9._+-]+ (passed|failed)( at [a-z]+)?$' "$ROW_DIR/ship.log" | head -1 | cut -d: -f1)
	[ -n "$first" ] || { row_fail "no check recorded a result: $(tail -3 "$ROW_DIR/ship.log" | tr '\n' ' ')"; return; }
	started=$(head -n "$first" "$ROW_DIR/ship.log" | grep -cE 'check-[0-9]+: attempt 1 of 3 on ')
	if [ "$started" -lt 2 ]; then
		row_fail "only $started check had started when the first recorded a result: they ran one at a time"
		return
	fi
	row_pass "ship of three ports ran $started checks together before the first result, within Tart's two VMs, in ${G13_TOOK:-?}s"
}
