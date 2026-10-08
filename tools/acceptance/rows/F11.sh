# stages: full
# F11: serve as a launchd agent in a fresh account reaching a Tart guest:
# the check runs. An inferred risk: macOS's Local Network privacy may
# prompt where no one can answer.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "serve as a launchd agent with Tart" || return 0
	allow_change "*"
	# The check is queued for serve to run: update's --check goes with
	# --outdated alone, and refused this row's setup (the rc8 full stage).
	dh_setup update "$(port)" --new || return 0
	# --fresh, so a guest runs under the agent rather than an earlier
	# build being reused (the rc9 full stage).
	dh_setup check -b "$(own_branch)" -d --fresh || return 0
	dh serve --install || return 0
	sleep 900
	dh_json status --port "$(port)" || :
	dh serve --uninstall || :
}
assert() {
	jq -e '.result.branches[0].latest_check.run.state | select(. == "passed" or . == "failed")' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "serve's agent ran the check in a guest" || row_fail "the agent's check didn't finish: Local Network privacy?"
}
