# stages: full
# D-R3: the dockhand image deleted, its golden copy kept: restored from
# the golden copy.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
# Its check names a branch of its own; and the image it deletes is a base
# image, dockhand-base-*, which dockhand-base-golden-gate, macOS 27's, is:
# filtering out "golden" anywhere left none on the M1 (the rc6 full stage).
setup() {
	host_only "dhtest's Tart images" || return 0
	dh_setup update "$(port)" --new || return 1
	DR3_BRANCH=$(own_branch)
}
act() {
	host_only "dhtest's Tart images" || return 0
	local image
	image=$(TART_HOME="${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" tart list --quiet 2>/dev/null | grep '^dockhand-base-' | head -1)
	[ -n "$image" ] || { row_fail "no dockhand image to delete"; return 0; }
	TART_HOME="${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" tart delete "$image"
	allow_change "*"
	dh_json check -b "$DR3_BRANCH" || :
}
assert() {
	grep -qi 'golden' "$ROW_DIR/out.log" && row_pass "restored from the golden copy" || row_fail "no restoration said"
}
