# stages: full
# D-R1: low disk, below cleanup.min_free, on a sparse image of a set
# size holding the database, and with it the logs and kept archives a
# check writes: cleanup runs at once and says so; a check that can't fit
# fails clearly, not half-written.
#
# Tart's home stays dockhand's own, with its images: a Tart home on the
# image had none, and check refused for want of one before the disk
# mattered (the rc9 full stage); an image is tens of gigabytes to copy.
. "$ROW_LIB/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a sparse disk image" || return 0
	local mount
	mount=$(fault_low_disk 60g) || { row_fail "the sparse image wasn't made"; return 0; }
	allow_change "*"
	# The branch is the image database's own, started there before the
	# disk fills: an empty database had none, and check stopped for want of
	# one before it touched the disk (the rc6 full stage).
	local branch
	branch=$(DOCKHAND_DB="$mount/dockhand.db" "$DH_BIN" --json update "$(port)" --new 2>>"$ROW_DIR/out.log" |
		jq -r '.result.branch | if type == "object" then .name else . end // empty')
	[ -n "$branch" ] || { row_fail "its branch couldn't be started on the image"; fault_low_disk_stop; return 0; }
	fault_low_disk_fill 2000
	DOCKHAND_DB="$mount/dockhand.db" "$DH_BIN" check -b "$branch" >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	fault_low_disk_stop
	rm -f "$ROW_DIR/lowdisk.sparseimage"
}
assert() {
	grep -qiE 'free|disk|space' "$ROW_DIR/out.log" && row_pass "said the disk was short" || row_fail "didn't say the disk was short"
}

# The image is detached whatever act did, and before the harm sweep reads
# what's left attached.
teardown() { fault_low_disk_stop; }
