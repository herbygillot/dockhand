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
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
DI5_STATE=${ACCEPT_STATE:-/nonexistent}/D-I5.state
act() {
	host_only "sleep and reboot" || return 0
	allow_change "*"
	if [ -f "$DI5_STATE" ]; then
		di5_after_reboot
		return 0
	fi
	dh_setup update "$(port)" --new || return 0
	DI5_BRANCH=$(own_branch)
	dh_bg check -b "$DI5_BRANCH" --fresh
	wait_for_line "$DH_BG_LOG" 'building|running' 900 || { row_fail "the check never started building"; kill "$DH_BG_PID" 2>/dev/null; return 0; }
	checkpoint "sleep the Mac (pmset sleepnow, or close the lid) for five minutes, then wake it" || { kill "$DH_BG_PID" 2>/dev/null; return 0; }
	dh_bg_wait || :
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
	judged "after the sleep the check resumed or failed clearly ($ROW_DIR/first-half.log); after the reboot serve picked the check up (status, serve's log); tart list ($ROW_DIR/vms) shows no orphaned VM"
}
