# stages: quick full
# exercises: the check's worker killed mid-check
# D-I6: the dockhand working a queued check killed. The next serve resumes
# what was queued, and nothing is left running (H7).
#
# The process killed is the one status says is working the check, not a
# serve the row started beside it: check -d starts its own drain, which
# may take the check first, and the row killed a serve that wasn't
# working it and passed on any exit (Codex's cold read at 2cdaf571,
# #232). The row proves its condition before it judges: the killed
# process held the check, the check was left unfinished, and a later
# serve took it up to its end.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

# di6_worker is the pid of the dockhand status says is working the check,
# or nothing.
di6_worker() {
	dh_queue | jq -r --arg run "$DI6_RUN" '.result.processes[]? | select(.runs | index($run)) | .pid' 2>/dev/null | head -1
}

# The row's own worker must be the one working the check: where dhtest's
# serve agent is loaded, it would take the check, and rc4's kill hit an
# idle process. The agent is set aside for the row and put back after.
setup() {
	if launchctl list 2>/dev/null | awk '$3 == "io.github.herbygillot.dockhand.serve"' | grep -q .; then
		"$DH_BIN" serve --uninstall >>"$ROW_DIR/out.log" 2>&1 || :
		touch "$ROW_DIR/agent-was-loaded"
	fi
	return 0
}
teardown() {
	[ ! -f "$ROW_DIR/agent-was-loaded" ] || "$DH_BIN" serve --install >>"$ROW_DIR/out.log" 2>&1 || :
	return 0
}
act() {
	dh_setup update "$(port)" --new || return 0
	dh_json check -p "$(port)" -d --fresh || return 0
	DI6_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json")
	[ -n "$DI6_RUN" ] || return 0
	# Deadlines by the wall clock, not by the sleeps between calls: on
	# dhtest's database a status call took up to a minute and a half, and
	# a limit counting only the sleeps would have spun for hours (rc4).
	local deadline=$((SECONDS + 900)) pid=""
	while [ "$SECONDS" -lt "$deadline" ]; do
		pid=$(di6_worker)
		[ -n "$pid" ] && break
		sleep 2
	done
	[ -n "$pid" ] || { row_result "not exercised" "no dockhand was working $DI6_RUN within 900s, so none could be killed mid-check"; return 0; }
	# The kill falls inside a build: status says the run is building
	# before it's sent (rc4's evidence read: a kill timed by a sleep hit
	# an idle process).
	local building=""
	deadline=$((SECONDS + 900))
	while [ "$SECONDS" -lt "$deadline" ]; do
		if dh_queue | jq -e --arg run "$DI6_RUN" '.result.queue[]? | select(.name == $run and ((.progress // "") | test("building in")))' >/dev/null 2>&1; then
			building=1
			break
		fi
		sleep 2
	done
	[ -n "$building" ] || { row_result "not exercised" "$DI6_RUN never said it was building in 900s, so no kill fell inside a build"; return 0; }
	pid=$(di6_worker)
	[ -n "$pid" ] || { row_result "not exercised" "no dockhand was working $DI6_RUN once it was building"; return 0; }
	printf '%s\n' "$pid" >"$ROW_DIR/killed.pid"
	printf '# killing %s, the dockhand working %s\n' "$pid" "$DI6_RUN" >>"$ROW_DIR/out.log"
	kill -9 "$pid" 2>/dev/null && echo killed >"$ROW_DIR/killed"
	sleep 2
	# The check is left unfinished by the kill, as status reads it.
	dh_queue | jq -r --arg run "$DI6_RUN" '.result.queue[]? | select(.name == $run) | .state' >"$ROW_DIR/after-kill.state" 2>/dev/null || :
	DI6_SERVED=$(log_size "$ROW_DIR/out.log")
	dh serve --drain || :
	dh_json wait "$DI6_RUN" || :
}

assert() {
	[ -n "${DI6_RUN:-}" ] || { row_fail "no check was queued"; return; }
	[ -f "$ROW_DIR/result" ] && return 0
	[ -f "$ROW_DIR/killed" ] || { row_result "not exercised" "the dockhand working $DI6_RUN (pid $(cat "$ROW_DIR/killed.pid" 2>/dev/null)) had ended before the kill"; return; }
	if [ ! -s "$ROW_DIR/after-kill.state" ]; then
		row_result "not exercised" "$DI6_RUN had finished by the kill, so no check was left to resume"
		return
	fi
	exercised "the check's worker killed mid-check"
	tail -c +"$((${DI6_SERVED:-0} + 1))" "$ROW_DIR/out.log" | grep -q "$DI6_RUN" ||
		{ row_fail "the serve after the kill never named $DI6_RUN, left $(cat "$ROW_DIR/after-kill.state")"; return; }
	case "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" in
	0 | 2) row_pass "the dockhand working $DI6_RUN was killed with it $(cat "$ROW_DIR/after-kill.state"); the next serve took it up to its end ($(jq -r '.result.run.state // empty' "$ROW_DIR/json/2.json"))" ;;
	*) row_fail "$DI6_RUN didn't finish after its worker was killed: exit $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" ;;
	esac
}
