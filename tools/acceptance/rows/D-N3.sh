# stages: full
# D-N3: a slow, stalled host. With Network Link Conditioner throttling,
# or the fault kit's stalling proxy, dockhand gives up after 3 minutes of
# no data, saying so.
. "$ROW_LIB/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a stalled network on the host" || return 0
	local proxy
	# The stall falls in a body, after TLS's handshake and the response's
	# headers have come through: a stall before the handshake met Go's
	# ten-second handshake timeout, not the three minutes without data
	# the row is for (Codex's cold read at 2cdaf571, #232).
	proxy=$(fault_proxy stall-body -after 65536) || { row_fail "the stalling proxy didn't start"; return 0; }
	SECONDS=0
	env -u NO_PROXY -u no_proxy HTTPS_PROXY="$proxy" https_proxy="$proxy" with_timeout 600 "$DH_BIN" update "$(port)" --plan >>"$ROW_DIR/out.log" 2>&1 || :
	printf '%s\n' "$SECONDS" >"$ROW_DIR/seconds"
	fault_proxy_stop
}
assert() {
	local s
	s=$(cat "$ROW_DIR/seconds" 2>/dev/null || echo 0)
	# The stall began after headers, as the proxy says, or nothing tested
	# the three minutes.
	grep -q '^.*stalled .* after [0-9]* bytes' "$ROW_DIR/faultproxy.log" 2>/dev/null ||
		{ row_result "not exercised" "no download ran past the proxy's first 64 KiB, so no stall fell in a body: $(tail -3 "$ROW_DIR/faultproxy.log" 2>/dev/null | tr '\n' ' ')"; return; }
	if [ "$s" -ge 590 ]; then
		row_fail "still waiting after ten minutes"
	elif [ "$s" -lt 150 ]; then
		row_fail "gave up after ${s}s, well before three minutes without data: $(grep -iE 'no data|stalled|gave up|timed out' "$ROW_DIR/out.log" | head -1)"
	elif grep -qiE 'no data|stalled|gave up|timed out' "$ROW_DIR/out.log"; then
		row_pass "gave up after ${s}s, saying so"
	else
		row_fail "ended after ${s}s without saying it stalled"
	fi
}
