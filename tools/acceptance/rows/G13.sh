# stages: full
# G13: a batch's checks run together (v0.7.0). With no serve leading,
# ship of three small outdated ports, --yes --to check --on tart, queues
# the three checks, then runs them together within Tart's two VMs, where
# they ran one at a time in input order: a second check starts before the
# first records a result, and each change is finished as its check ends.
# ACCEPT_BATCH_PORTS names the three, the day's, since only a person
# knows which are outdated and small; without it the row isn't run, nor
# where outdated says any of them is current, naming which: a stale list
# ran ship of one due port and failed the row (rc4's full stage,
# influx-cli and monolith current). It
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
	local current
	current=$("$DH_BIN" --json outdated "$@" 2>/dev/null | jq -r '.result.ports[]? | select(.outdated != true) | .port' | tr '\n' ' ' | sed 's/ $//')
	if [ -n "$current" ]; then
		row_result "not run" "ACCEPT_BATCH_PORTS is stale: outdated finds no newer release of $current; name three small ports that are due"
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

# g13_waiters names each check a ship.log says is waiting, by the prefix
# ship gives its line, "<branch or check>: waits for a slot on ..." or
# "<branch or check>: waiting for the Mac's VMs: ...", never a holder the
# line names after it.
g13_waiters() {
	grep -E "^[^ :]+: (waiting for the Mac's VMs|waits for a slot)" "$1" | sed -E 's/^([^ :]+): .*/\1/' | sort -u
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/ship.log" ] || return 0
	local exit started
	exit=$(sed -n 's/^\[exit \([0-9]*\)\]$/\1/p' "$ROW_DIR/ship.log" | tail -1)
	case "$exit" in
	0 | 3) ;;
	*) row_fail "ship exited $exit: $(grep -v '^\[exit' "$ROW_DIR/ship.log" | tail -2 | tr '\n' ' ')"; return ;;
	esac
	# The checks ship named, and their first executions' times, from the
	# stage's database: ship says each check once it ends, so its text
	# read three checks run side by side as one at a time (rc1's full
	# stage, check-820 to 822).
	local numbers rows
	numbers=$(grep -oE 'check-[0-9]+: attempt 1 of 3' "$ROW_DIR/ship.log" | sed 's/check-//; s/:.*//' | sort -un | tr '\n' ',' | sed 's/,$//')
	[ -n "$numbers" ] || { row_fail "ship named no check: $(tail -3 "$ROW_DIR/ship.log" | tr '\n' ' ')"; return; }
	rows=$(stage_db "SELECT r.number, e.created_at, coalesce(e.finished_at, 0) FROM executions e JOIN runs r ON r.repository_id = e.repository_id AND r.id = e.run_id WHERE r.number IN ($numbers) AND e.attempt = 1 ORDER BY e.created_at" 2>>"$ROW_DIR/out.log")
	printf '%s\n' "$rows" >"$ROW_DIR/executions.txt"
	[ "$(printf '%s\n' "$rows" | grep -c '|')" -ge 3 ] || { row_fail "the stage's database has $(printf '%s\n' "$rows" | grep -c '|') of the three checks' executions: $rows"; return; }
	# How many started before the first to finish ended. An execution is
	# recorded before it waits for a slot or one of the Mac's VMs, which
	# the batch says (ec5a4846), so a check said waiting doesn't count. The
	# waiter is the line's prefix, what ship names the check by, a branch
	# or a check: the first check-N in the line can be a holder, as both
	# wordings name the holders after the waiter (rc4's reruns counted
	# all three as waiting).
	local waited="" waiter number
	while IFS= read -r waiter; do
		[ -n "$waiter" ] || continue
		case "$waiter" in
		check-[0-9]*) number=${waiter#check-} ;;
		*) number=$(stage_db "SELECT r.number FROM runs r JOIN branches b ON b.repository_id = r.repository_id AND b.id = r.branch_id WHERE r.number IN ($numbers) AND (b.name = '$waiter' OR b.name = 'dockhand/$waiter')" 2>>"$ROW_DIR/out.log") ;;
		esac
		waited="$waited $number"
	done <<EOT
$(g13_waiters "$ROW_DIR/ship.log")
EOT
	started=$(printf '%s\n' "$rows" | awk -F'|' -v waited=" $waited " '
		$3 > 0 && (end == 0 || $3 < end) { end = $3 }
		index(waited, " " $1 " ") == 0 { start[NR] = $2 }
		END { n = 0; for (i in start) if (start[i] < end) n++; print n }')
	if [ "$started" -lt 2 ]; then
		row_fail "only $started check had started when the first finished: they ran one at a time ($ROW_DIR/executions.txt)"
		return
	fi
	row_pass "ship of three ports had $started checks building together before the first finished, within Tart's two VMs, in ${G13_TOOK:-?}s (by the stage's database)"
}
