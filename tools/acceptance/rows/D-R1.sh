# stages: full
# D-R1: free space below cleanup.min_free: the next command starts cleanup
# at once, saying how much is free, where, and under what floor.
#
# Tested through the floor, set above what's free for the row, rather than
# by filling a disk: a check builds where Tart's home and its clones are,
# tens of gigabytes that a small disk image can't hold, and the image the
# rc9 and rc10 runs made held only the database, so the disk was never
# short where the build was (the rc10 full stage). A check that can't fit
# failing clearly, the row's other half, isn't covered here.
#
# Cleanup on low space waits an hour after the last pass (LowSpacePause),
# so the stamp is set two hours back for the row, and put back after.

setup() {
	host_only "dhtest's own configuration and cleanup" || return 0
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/config.before"
	DR1_STAMP=$(find "$HOME/.dockhand/serve" -name cleanup.stamp 2>/dev/null | head -1)
	if [ -n "$DR1_STAMP" ]; then
		cp -p "$DR1_STAMP" "$ROW_DIR/cleanup.stamp.before"
	fi
}

act() {
	host_only "dhtest's own configuration and cleanup" || return 0
	if [ -z "${DR1_STAMP:-}" ]; then
		row_result "not run" "no cleanup stamp under ~/.dockhand/serve to set back, so free space wouldn't be what starts cleanup"
		return 0
	fi
	allow_change "*"
	# A floor no Mac has free: a thousand terabytes.
	if grep -q '^\[cleanup\]' "$DOCKHAND_CONFIG"; then
		sed '/^\[cleanup\]/a\
min_free = "1000TB"
/^min_free *=/d' "$ROW_DIR/config.before" >"$DOCKHAND_CONFIG"
	else
		printf '\n[cleanup]\nmin_free = "1000TB"\n' >>"$DOCKHAND_CONFIG"
	fi
	touch -t "$(date -v-2H +%Y%m%d%H%M.%S)" "$DR1_STAMP"
	dh status || :
}

assert() {
	[ -n "${DR1_STAMP:-}" ] || return 0
	grep -qE 'Cleaning up in the background: only .* free where .*, under ' "$ROW_DIR/out.log" &&
		row_pass "cleanup started at once, saying what's free, where, and the floor: $(grep -oE 'Cleaning up in the background: [^.]*' "$ROW_DIR/out.log" | head -1)" ||
		row_fail "free space under cleanup.min_free didn't start cleanup, or didn't say so"
}

# The configuration and the stamp go back as they were.
teardown() {
	[ -f "$ROW_DIR/config.before" ] && cp "$ROW_DIR/config.before" "$DOCKHAND_CONFIG"
	[ -n "${DR1_STAMP:-}" ] && [ -f "$ROW_DIR/cleanup.stamp.before" ] && cp -p "$ROW_DIR/cleanup.stamp.before" "$DR1_STAMP"
	return 0
}
