# stages: full
# G4: a check in /opt/local that ends mid-build, dockhand and its guest
# program both killed, as a Mac losing power would: status names dockhand
# undo check-N, which puts the active ports back exactly, where port
# restore left unrequested ones inactive (rc8's G4), and says a check of
# the branch first needs it waited for, replaced, or cancelled.
# ACCEPT_SLEEP_PORT's build lasts long enough to be caught in.
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
	G4_RUN=$(grep -o 'dockhand undo check-[0-9]*' "$ROW_DIR/status.txt" | awk '{print $NF}' | head -1)
	[ -z "$G4_RUN" ] || { local_root && dh undo "$G4_RUN" </dev/null; } || :
	dh cancel -b "$G4_BRANCH" || :
	"$DH_BIN" status >"$ROW_DIR/status.after" 2>&1 </dev/null || :
	cat "$ROW_DIR/status.after" >>"$ROW_DIR/out.log"
}

assert() {
	[ -n "${G4_BRANCH:-}" ] || return 0
	[ -n "${G4_RUN:-}" ] || { row_fail "status didn't name the dockhand undo that puts /opt/local back: $(grep -i 'local' "$ROW_DIR/status.txt" | head -2 | tr '\n' ' ')"; return; }
	grep -q 'stopped before it could restore /opt/local' "$ROW_DIR/status.txt" || { row_fail "status didn't say the check left /opt/local changed"; return; }
	grep -q "first needs dockhand wait $G4_RUN, check --replace, or dockhand cancel $G4_RUN" "$ROW_DIR/status.txt" || { row_fail "status didn't say a check of the branch first needs $G4_RUN settled"; return; }
	local_same /opt/local "$ROW_DIR/active.before" "dockhand undo $G4_RUN didn't put /opt/local back exactly" || return
	! grep -q 'stopped before it could restore /opt/local' "$ROW_DIR/status.after" || { row_fail "status still said /opt/local was changed once it was put back"; return; }
	row_pass "status named dockhand undo $G4_RUN, which put /opt/local back exactly, and status then said nothing of it"
}
