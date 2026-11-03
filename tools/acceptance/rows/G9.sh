# stages: full
# G9: a foreground check in /opt/local with no sudo rule that asks no
# password, its build outlasting sudo's five minutes, its guest program
# killed while dockhand runs (v0.7.0's put-back): dockhand asks for
# dhtest's password again, on the terminal, as at the check's start, and
# puts /opt/local back exactly. ACCEPT_G9_PORT's build is the long one,
# nodejs22 unless set, since the kill waits six minutes into it and
# dominant_colours (1m35s) and rust-analyzer (2m33s) ended first on the
# M1 (rc7's G9, 2026-11-02); a person kills the program from another
# terminal, whose sudo leaves this terminal's ticket to expire. From
# v0.8.0 a guest killed so isn't tried again, and the check says the kill;
# v0.7.0 builds again, asking the password at each attempt.
. "${ROW_LIB:?}/local.sh"

g9_port() { printf '%s' "${ACCEPT_G9_PORT:-nodejs22}"; }

# g9_stops says whether the dockhand under test stops at a kill rather
# than trying again: v0.8.0 and later, and a development build.
g9_stops() {
	local version
	version=$("$DH_BIN" --version 2>/dev/null | awk '{print $2}')
	case "$version" in "" | devel*) return 0 ;; esac
	[ "$(printf '%s\n0.8\n' "${version#v}" | sort -V | head -1)" = 0.8 ]
}

act() {
	host_only "a foreground check in /opt/local, at dhtest's terminal" || return 0
	checkpoint "remove every sudo rule of dhtest's that asks no password, G8's among them, and stay at the terminal full.sh runs in: G9's check asks dhtest's password at its start and again at its put-back, and you'll be asked to kill its program from another terminal" || return 0
	if sudo -n /opt/local/bin/port-tclsh /dev/null 2>/dev/null; then
		row_result "not run" "sudo still runs /opt/local/bin/port-tclsh with no password, so nothing would be asked again"
		return 0
	fi
	[ -t 0 ] || { row_result "not run" "full.sh has no terminal for the password"; return 0; }
	G9_RAN=1
	local_active /opt/local >"$ROW_DIR/active.before"
	G9_BRANCH=$(run_name g9-again)
	dh_setup revbump "$(g9_port)" --new="$G9_BRANCH" --subject "rebuild to outlast sudo's five minutes in /opt/local" || return 0
	g9_kill &
	local watcher=$!
	dh check -b "$G9_BRANCH" --on local --fresh || :
	wait "$watcher" 2>/dev/null || :
}

# g9_kill asks for the guest program to be killed once its build has run
# six minutes past root's ticket, which then has expired.
g9_kill() {
	local from dir pid
	from=$(log_size "$ROW_DIR/out.log")
	wait_for_line "$ROW_DIR/out.log" 'building in /opt/local \(as root\)' 1800 "$from" || return 0
	sleep 360
	grep -q 'put /opt/local back as it was' "$ROW_DIR/out.log" && return 0
	dir=$(ls -dt /var/tmp/dockhand-local-* 2>/dev/null | head -1)
	pid=$(jq -r '.environment.pid // empty' "$dir/results.json" 2>/dev/null)
	[ -n "$pid" ] || return 0
	local after="the check puts /opt/local back, says the kill, and makes no other attempt"
	g9_stops || after="the check puts /opt/local back and, as v0.7.0 does, builds again, asking the password at each attempt; kill only this attempt"
	checkpoint "from another terminal, kill G9's guest program and its builds: sudo kill -9 -- -$(ps -o pgid= -p "$pid" | tr -d ' ') $pid; then answer done here, and type dhtest's password at full.sh's terminal when the check asks: $after" || return 0
}

assert() {
	[ -n "${G9_RAN:-}" ] || return 0
	[ -f "$ROW_DIR/result" ] && return 0
	local asked
	asked=$(grep -c "so sudo asks for your password" "$ROW_DIR/out.log")
	if [ "$asked" -lt 2 ]; then
		row_result "not run" "root was asked for once: the build ended before sudo's five minutes, or the program wasn't killed after them; a longer ACCEPT_G9_PORT than $(g9_port) shows it"
		return
	fi
	grep -q 'put /opt/local back as it was' "$ROW_DIR/out.log" || { row_fail "the check didn't put /opt/local back after asking for root again: $(grep "wasn't put back" "$ROW_DIR/out.log" | tail -1)"; return; }
	local_same /opt/local "$ROW_DIR/active.before" "the put-back left /opt/local's active ports changed" || return
	local builds
	builds=$(grep -c 'building in /opt/local (as root)' "$ROW_DIR/out.log")
	if g9_stops; then
		grep -q "was killed by a signal dockhand didn't send" "$ROW_DIR/out.log" || { row_fail "the check didn't say the guest program was killed by a signal it didn't send: $(grep -E 'guest program|attempt' "$ROW_DIR/out.log" | tail -2 | tr '\n' ' ')"; return; }
		[ "$builds" = 1 ] || { row_fail "the check built $builds times; a killed guest program isn't tried again"; return; }
		row_pass "the check asked for root again at its put-back, past sudo's five minutes, put /opt/local back exactly, said the kill, and made no other attempt"
		return
	fi
	row_pass "the check asked for root again at its put-back, past sudo's five minutes, and put /opt/local back exactly; as v0.7.0 does, it built again after the kill ($builds builds)"
}
