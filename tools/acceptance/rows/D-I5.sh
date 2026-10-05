# stages: full
# D-I5: the Mac sleeps during a check, and reboots while serve runs. The
# check resumes or fails clearly, with no VM orphaned; serve picks the
# check up after login. FileVault stays on, so a person unlocks: as the
# driver, since dhtest, made with sysadminctl, can't unlock FileVault.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "sleep and reboot" || return 0
	allow_change "*"
	dh_setup update "$(port)" --new || return 0
	DI5_BRANCH=$(dh_quiet --json status --port "$(port)" | jq -r '.result.branches[0].name')
	dh_bg check -b "$DI5_BRANCH"
	wait_for_line "$DH_BG_LOG" 'building|running' 900 || :
	checkpoint "sleep the Mac (pmset sleepnow, or close the lid) for five minutes, then wake it" || return 0
	dh_bg_wait || :
	dh serve --install || :
	dh check -b "$DI5_BRANCH" --fresh --enqueue || :
	checkpoint "reboot the Mac (sudo shutdown -r now), unlock FileVault as the admin driver (dhtest can't), and log dhtest in; then resume" || return 0
	sleep 120
	dh_json status "$DI5_BRANCH" || :
	dh serve --uninstall || :
}
assert() {
	judged "after the sleep the check resumed or failed clearly; after the reboot serve picked the check up; tart list shows no orphaned VM"
}
