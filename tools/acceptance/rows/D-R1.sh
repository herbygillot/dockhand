# stages: full
# D-R1: low disk, below cleanup.min_free, on a sparse image of a set
# size holding the database and Tart's home: cleanup runs at once and
# says so; a check that can't fit fails clearly, not half-written.
. "$ROW_LIB/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a sparse disk image" || return 0
	local mount
	mount=$(fault_low_disk 60g) || { row_fail "the sparse image wasn't made"; return 0; }
	fault_low_disk_fill 2000
	allow_change "*"
	DOCKHAND_DB="$mount/dockhand.db" DOCKHAND_TART_HOME="$mount/tart" "$DH_BIN" check -p "$(port)" >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	fault_low_disk_stop
	rm -f "$ROW_DIR/lowdisk.sparseimage"
}
assert() {
	grep -qiE 'free|disk|space' "$ROW_DIR/out.log" && row_pass "said the disk was short" || row_fail "didn't say the disk was short"
}
