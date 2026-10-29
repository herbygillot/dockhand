# stages: full
# G4: a check in /opt/local that ends mid-build, dockhand and its guest
# program both killed, as a Mac losing power would: status names the
# snapshot, and sudo port restore --snapshot-id N, as it says, puts the
# active ports back. ACCEPT_SLEEP_PORT's build lasts long enough to be
# caught in.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a check in /opt/local, as root" || return 0
	local_root || return 0
	local_active /opt/local >"$ROW_DIR/active.before"
	G4_BRANCH=$(run_name g4-killed)
	dh_setup revbump "${ACCEPT_SLEEP_PORT:?}" --new="$G4_BRANCH" --subject "rebuild to stop mid-build in /opt/local" || return 0
	dh_bg check -b "$G4_BRANCH" --on local
	if ! wait_for_line "$DH_BG_LOG" "building .*: (fetch|checksum|install)" 1800; then
		kill "$DH_BG_PID" 2>/dev/null
		dh_bg_wait || :
		row_fail "the check never reached its build in /opt/local"
		return 0
	fi
	# The guest program's process, which goes on where dockhand ends.
	local dir pid
	dir=$(ls -dt /var/tmp/dockhand-local-* 2>/dev/null | head -1)
	pid=$(jq -r '.environment.pid // empty' "$dir/results.json" 2>/dev/null)
	kill -9 "$DH_BG_PID" 2>/dev/null
	[ -z "$pid" ] || sudo /bin/kill -9 -- "-$(ps -o pgid= -p "$pid" | tr -d ' ')" "$pid" 2>/dev/null || :
	dh_bg_wait || :
	# dh sends dockhand's output to out.log alone, so status is run
	# directly to keep its own copy, then added to the log (the rc8 G4
	# run, whose status.txt was always empty).
	"$DH_BIN" status >"$ROW_DIR/status.txt" 2>&1 </dev/null || :
	cat "$ROW_DIR/status.txt" >>"$ROW_DIR/out.log"
	G4_SNAPSHOT=$(grep -o 'sudo port restore --snapshot-id [0-9]*' "$ROW_DIR/status.txt" | awk '{print $NF}' | head -1)
	[ -z "$G4_SNAPSHOT" ] || sudo /opt/local/bin/port -N restore --snapshot-id "$G4_SNAPSHOT" >>"$ROW_DIR/out.log" 2>&1 || :
	dh cancel -b "$G4_BRANCH" || :
	"$DH_BIN" status >"$ROW_DIR/status.after" 2>&1 </dev/null || :
	cat "$ROW_DIR/status.after" >>"$ROW_DIR/out.log"
}

assert() {
	[ -n "${G4_BRANCH:-}" ] || return 0
	[ -n "${G4_SNAPSHOT:-}" ] || { row_fail "status didn't name the snapshot that puts /opt/local back: $(grep -i 'local' "$ROW_DIR/status.txt" | head -2 | tr '\n' ' ')"; return; }
	grep -q 'stopped before it could restore /opt/local' "$ROW_DIR/status.txt" || { row_fail "status didn't say the check left /opt/local changed"; return; }
	local_same /opt/local "$ROW_DIR/active.before" "port restore --snapshot-id $G4_SNAPSHOT didn't put /opt/local back" || return
	! grep -q 'stopped before it could restore /opt/local' "$ROW_DIR/status.after" || { row_fail "status still said /opt/local was changed once it was put back"; return; }
	row_pass "status named snapshot $G4_SNAPSHOT, port restore put /opt/local back, and status then said nothing of it"
}
