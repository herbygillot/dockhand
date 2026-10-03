# stages: full
# D-N3: a slow, stalled host. With Network Link Conditioner throttling,
# or the fault kit's stalling proxy, dockhand gives up after 3 minutes of
# no data, saying so.
. "$ROW_LIB/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a stalled network on the host" || return 0
	local proxy
	proxy=$(fault_proxy stall) || { row_fail "the stalling proxy didn't start"; return 0; }
	SECONDS=0
	HTTPS_PROXY=$proxy with_timeout 600 "$DH_BIN" update "$(port)" --plan >>"$ROW_DIR/out.log" 2>&1 || :
	printf '%s\n' "$SECONDS" >"$ROW_DIR/seconds"
	fault_proxy_stop
}
assert() {
	local s
	s=$(cat "$ROW_DIR/seconds" 2>/dev/null || echo 0)
	if [ "$s" -ge 590 ]; then
		row_fail "still waiting after ten minutes"
	elif grep -qiE 'no data|stalled|gave up|timed out' "$ROW_DIR/out.log"; then
		row_pass "gave up after ${s}s, saying so"
	else
		row_fail "ended after ${s}s without saying it stalled"
	fi
}
