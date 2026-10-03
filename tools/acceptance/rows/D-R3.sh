# stages: full
# D-R3: the dockhand image deleted, its golden copy kept: restored from
# the golden copy.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "dhtest's Tart images" || return 0
	local image
	image=$(TART_HOME="${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" tart list --quiet 2>/dev/null | grep '^dockhand-base-' | grep -v golden | head -1)
	[ -n "$image" ] || { row_fail "no dockhand image to delete"; return 0; }
	TART_HOME="${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" tart delete "$image"
	allow_change "*"
	dh_json check -p "$(port)" || :
}
assert() {
	grep -qi 'golden' "$ROW_DIR/out.log" && row_pass "restored from the golden copy" || row_fail "no restoration said"
}
