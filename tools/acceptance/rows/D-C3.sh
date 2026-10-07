# stages: full
# D-C3: dockhand's authorization revoked on GitHub: the next command
# needing it says the login was revoked, not a raw 401 (H4 holds), once;
# and serve, running across it, says once it can't act, as status says
# beside it, and acts again on the new login, with no restart.
act() {
	host_only "a GitHub authorization to revoke" || return 0
	local log=$HOME/.dockhand/logs/serve.log before
	before=$(wc -c <"$log" 2>/dev/null | tr -d ' ' || echo 0)
	dh serve --install || return 0
	sleep 20
	dh_quiet --json queue | jq -r '.result.serve_state.pid // empty' >"$ROW_DIR/pid.before"
	checkpoint "revoke dockhand's authorization on the test account (GitHub, Settings, Applications)" || return 0
	dh_json status --refresh || :
	# serve reads who its login is each minute, and asks GitHub again for
	# the same token each ten.
	wait_for_line "$log" '^serve: GitHub rejected its login' 720 || :
	dh_json queue || :
	# A revoked login is dead, so the rows after need a new one; it's
	# asked for here, beside the revocation, where the person already is.
	device_login "D-C3 logs dockhand in again after the revocation" || :
	wait_for_line "$log" '^serve: logged in again as ' 180 || :
	dh_quiet --json queue | jq -r '.result.serve_state.pid // empty' >"$ROW_DIR/pid.after"
	tail -c +"$((${before:-0} + 1))" "$log" >"$ROW_DIR/serve.log" 2>/dev/null || :
	dh serve --uninstall || :
}
teardown() {
	"$DH_BIN" serve --uninstall >/dev/null 2>&1 || :
}
assert() {
	local status queue
	status=$(grep -l '^status --refresh$' "$ROW_DIR"/json/*.json.args 2>/dev/null | head -1)
	queue=$(grep -l '^queue$' "$ROW_DIR"/json/*.json.args 2>/dev/null | head -1)
	jq -r '.error // empty' "${status%.args}" | grep -qi 'rejected' && ! grep -q '401' "$ROW_DIR/out.log" ||
		{ row_fail "not said as rejected: $(jq -r '.error // empty' "${status%.args}")"; return; }
	[ "$(grep -c 'rejected the credential' "$ROW_DIR/out.log")" -le 2 ] ||
		{ row_fail "the rejection was said more than once"; return; }
	[ "$(grep -c '^serve: GitHub rejected its login' "$ROW_DIR/serve.log")" = 1 ] ||
		{ row_fail "serve didn't say once that GitHub rejected its login"; return; }
	jq -r '.result.serve_state.not_acting // empty' "${queue%.args}" | grep -q 'rejected' ||
		{ row_fail "queue didn't say serve wasn't acting on GitHub"; return; }
	grep -q '^serve: logged in again as ' "$ROW_DIR/serve.log" ||
		{ row_fail "serve didn't say its login was back"; return; }
	cmp -s "$ROW_DIR/pid.before" "$ROW_DIR/pid.after" ||
		{ row_fail "serve didn't run on as one process: pid $(cat "$ROW_DIR/pid.before") before, $(cat "$ROW_DIR/pid.after") after"; return; }
	row_pass "said the login was rejected, once; serve said so once, queue said it wasn't acting, and it acted again on the new login as one process"
}
