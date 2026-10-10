# stages: full
# order: alone
# D-I5: the Mac sleeps during a check, and reboots while serve runs one.
# The check resumes or fails clearly, with no VM orphaned; serve picks the
# check up after login (Design v3 §11). FileVault stays on, so a person
# unlocks: as the driver, since dhtest, made with sysadminctl, can't
# unlock FileVault.
#
# It runs alone, in two halves around the reboot, since the reboot ends
# the run that's going: `full.sh --rows D-I5` until the reboot, which
# keeps the branch in $ACCEPT_STATE/D-I5.state, then the same again after
# login, which finds it and judges what serve did. Its checks are --fresh:
# one that reused an earlier build ended in seconds, before the sleep it
# was to be interrupted by (the rc6 full stage).
#
# The sleep must fall within the check, which a person's answer can't
# promise: on rc10 the Go port's check finished at 01:51Z and the sleep
# came at 02:30Z, testing nothing. So the check starts only once the
# person says they're there, builds ACCEPT_SLEEP_PORT, a longer build,
# is confirmed running right before the sleep is asked for, and
# afterwards pmset's log must show a sleep between the check's building
# and its end; without one, the sleep half is "not run", with why.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
sleep_port() { printf '%s' "${ACCEPT_SLEEP_PORT:-${ACCEPT_RUST_PORT:?}}"; }
DI5_STATE=${ACCEPT_STATE:-/nonexistent}/D-I5.state
# Why the sleep half saw nothing, kept for the second half's verdict,
# since the run after the reboot starts the row's results afresh.
DI5_SLEPT=${ACCEPT_STATE:-/nonexistent}/D-I5.sleep
act() {
	host_only "sleep and reboot" || return 0
	allow_change "*"
	if [ -f "$DI5_STATE" ]; then
		di5_after_reboot
		return 0
	fi
	rm -f "$DI5_SLEPT"
	di5_sleep || return 0
	dh_setup update "$(port)" --new || return 0
	# The row's newest branch but the sleep's.
	DI5_BRANCH=$("$DH_BIN" --json status 2>/dev/null | jq -r '.result.branches[]?.name' |
		grep -vxF -f "$ROW_DIR/branches.before" | grep -vxF "${DI5_SLEEP_BRANCH:-}" | tail -1)
	[ -n "$DI5_BRANCH" ] || { row_fail "the update made no branch"; return 0; }
	local served
	served=$(log_size "$HOME/.dockhand/logs/serve.log")
	dh serve --install || :
	dh check -b "$DI5_BRANCH" --fresh -d || :
	# serve has the check building before the reboot ends it, said since
	# this serve began: serve.log keeps earlier runs' lines.
	wait_for_line "$HOME/.dockhand/logs/serve.log" "check-[0-9]+ $DI5_BRANCH: (running|resuming)" 600 "$served" || :
	printf '%s\n' "$DI5_BRANCH" >"$DI5_STATE"
	# The second half's run sets earlier rows' branches aside, but this one,
	# and starts the row's results afresh, so the first half's log is kept.
	printf '%s\n' "$DI5_BRANCH" >>"$ACCEPT_STATE/keep-branches"
	cp "$ROW_DIR/out.log" "$ACCEPT_STATE/D-I5.first.log"
	row_result "not run" "the first half ran to the reboot: reboot the Mac (sudo shutdown -r now), unlock FileVault as the admin driver, log dhtest in, and run full.sh --rows D-I5 again"
	printf 'WAITING: reboot the Mac (sudo shutdown -r now), unlock FileVault as the admin driver (dhtest can'"'"'t), log dhtest in, then run full.sh --rows D-I5 again for the second half\n' >&3
}

# di5_sleep is the sleep half: a check of the sleep port's own branch,
# with the Mac slept while it builds. It says whether the row goes on; a
# sleep that missed the check is kept in $DI5_SLEPT, said as not run by
# the second half, and the reboot half still runs.
di5_sleep() {
	checkpoint "say ready to sleep the Mac within a few minutes of being asked (D-I5 starts a check, then asks)" || return 1
	dh_setup update "$(sleep_port)" --new || return 1
	local started ended
	DI5_SLEEP_BRANCH=$(own_branch)
	[ -n "$DI5_SLEEP_BRANCH" ] || { row_fail "the sleep port's update made no branch"; return 1; }
	dh_bg check -b "$DI5_SLEEP_BRANCH" --fresh
	wait_for_line "$DH_BG_LOG" 'building|running' 900 || { row_fail "the check never started building"; kill "$DH_BG_PID" 2>/dev/null; return 1; }
	started=$(date '+%Y-%m-%d %H:%M:%S')
	# Asked only of a check still running: one already done has nothing
	# for the sleep to interrupt.
	if ! kill -0 "$DH_BG_PID" 2>/dev/null; then
		dh_bg_wait || :
		printf '%s\n' "the sleep port's check ended before the sleep could be asked for; set ACCEPT_SLEEP_PORT to a port with a longer build" >"$DI5_SLEPT"
		return 0
	fi
	checkpoint "sleep the Mac now (pmset sleepnow, or close the lid) for five minutes, then wake it; the check is building" || { kill "$DH_BG_PID" 2>/dev/null; return 1; }
	dh_bg_wait || :
	ended=$(date '+%Y-%m-%d %H:%M:%S')
	pmset -g log 2>/dev/null | awk -v from="$started" -v to="$ended" '$4 == "Sleep" && ($1 " " $2) >= from && ($1 " " $2) <= to' >"$ROW_DIR/sleeps"
	if [ ! -s "$ROW_DIR/sleeps" ]; then
		printf '%s\n' "pmset's log shows no sleep between the check's building ($started) and its end ($ended), so a check across a sleep wasn't seen" >"$DI5_SLEPT"
	fi
	return 0
}

# di5_after_reboot is the second half: what serve did with the check the
# reboot stopped.
di5_after_reboot() {
	DI5_BRANCH=$(cat "$DI5_STATE")
	rm -f "$DI5_STATE"
	sed -i '' "/^$DI5_BRANCH\$/d" "$ACCEPT_STATE/keep-branches" 2>/dev/null || :
	mv "$ACCEPT_STATE/D-I5.first.log" "$ROW_DIR/first-half.log" 2>/dev/null || :
	# serve starts at login, and picks stopped work up.
	sleep 180
	dh_json status "$DI5_BRANCH" || :
	tart list >"$ROW_DIR/vms" 2>&1 || :
	dh serve --uninstall || :
	DI5_SECOND=1
}
teardown() {
	# The first half leaves serve running across the reboot, on purpose.
	[ -f "$DI5_STATE" ] && return 0
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
	return 0
}
assert() {
	[ -n "${DI5_SECOND:-}" ] || return 0
	if [ -f "$DI5_SLEPT" ]; then
		row_result "not run" "the sleep half: $(cat "$DI5_SLEPT"); the reboot half is for a person to judge: serve's log, status, and $ROW_DIR/vms"
		rm -f "$DI5_SLEPT"
		return
	fi
	judged "after the sleep the check resumed or failed clearly ($ROW_DIR/first-half.log); after the reboot serve picked the check up (status, serve's log); tart list ($ROW_DIR/vms) shows no orphaned VM"
}
