# stages: full
# D-R4: Tart, or MacPorts, gone after setup, hidden by a PATH shim: refused
# with what's missing; nothing else breaks.
. "$ROW_LIB/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
# Its check names a branch of its own, and its update plans from master:
# with earlier rows' branches set aside, both were refused for want of a
# branch before they missed Tart or MacPorts (the rc6 full stage).
setup() {
	host_only "a set-up Tart and MacPorts" || return 0
	dh_setup update "$(port)" --new || return 1
	DR4_BRANCH=$(own_branch)
}
act() {
	host_only "a set-up Tart and MacPorts" || return 0
	local shim
	shim=$(fault_shims tart)
	# --fresh, so a reused build doesn't skip the tart the shim is.
	PATH="$shim:$PATH" "$DH_BIN" check -b "$DR4_BRANCH" --fresh >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	rm -rf "$shim"
	shim=$(fault_shims port)
	PATH="$shim:$PATH" "$DH_BIN" update "$(port)" --new --plan >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	dh_json status || :
}
assert() {
	grep -qi 'tart' "$ROW_DIR/out.log" && grep -qiE 'macports|port ' "$ROW_DIR/out.log" || { row_fail "what's missing wasn't named"; return; }
	[ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ] && row_pass "each refused naming what's missing, and status still works" || row_fail "status broke"
}
