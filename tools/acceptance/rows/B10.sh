# stages: quick full
# B10: after a real failed check, on a deliberately broken local edit of
# the small Go port (its destroot fails): logs, retry, check --baseline,
# queue, wait, and cancel each find the right run, and the baseline says
# whether master fails too.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

act() {
	dh start b10-broken --port "$(port)" || return 0
	local dir portfile
	dir=$("$DH_BIN" path b10-broken) || return 0
	portfile=$(find "$dir" -path "*/$(port)/Portfile" | head -1)
	printf '\npost-destroot {\n    return -code error "acceptance: a deliberate failure"\n}\n' >>"$portfile"
	allow_change "$portfile"
	dh_json check -b b10-broken || :
	B10_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json")
	[ -n "$B10_RUN" ] || return 0
	dh logs "$B10_RUN" || :
	dh_json retry "$B10_RUN" || :
	dh_json check -b b10-broken --baseline || :
	dh_json check -b b10-broken -d || :
	B10_QUEUED=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/4.json")
	dh queue || :
	[ -n "$B10_QUEUED" ] && dh_json cancel "$B10_QUEUED" || :
	dh_json wait "$B10_RUN" || :
}

assert() {
	local exit
	exit=$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)
	if [ "$exit" != 2 ]; then
		row_fail "the broken check exited ${exit:-nothing}, not 2 for a failed check"
		return
	fi
	if ! sed -n "/^\\\$ dockhand logs $B10_RUN/,/^\\[exit/p" "$ROW_DIR/out.log" | grep -q '^\[exit 0\]'; then
		row_fail "logs $B10_RUN didn't find the run"
		return
	fi
	[ "$(cat "$ROW_DIR/json/2.json.exit")" = 2 ] || { row_fail "retry exited $(cat "$ROW_DIR/json/2.json.exit"), not 2"; return; }
	if ! jq -e '.result.run != null' "$ROW_DIR/json/3.json" >/dev/null; then
		row_fail "the baseline ran nothing: $(jq -r '.error // empty' "$ROW_DIR/json/3.json")"
		return
	fi
	[ "$(cat "$ROW_DIR/json/5.json.exit" 2>/dev/null)" = 0 ] || { row_fail "cancel of the queued $B10_QUEUED exited $(cat "$ROW_DIR/json/5.json.exit" 2>/dev/null)"; return; }
	[ "$(cat "$ROW_DIR/json/6.json.exit" 2>/dev/null)" = 2 ] || { row_fail "wait on the failed $B10_RUN exited $(cat "$ROW_DIR/json/6.json.exit" 2>/dev/null), not 2"; return; }
	row_pass "logs, retry, the baseline ($(jq -r '.result.run.state // "?"' "$ROW_DIR/json/3.json")), queue, cancel, and wait each found the right run"
}
