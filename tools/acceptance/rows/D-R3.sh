# stages: full
# D-R3: the dockhand image deleted, its golden copy kept: check says what
# restores it, and setup tart restores it from the golden copy, in a
# minute or so, not the hour a new one takes.
#
# What's asserted is the restore, by setup's own words and the image
# there after, where a grep for "golden" also matched check's refusal,
# "macOS 27 (Golden Gate)", and passed a row that restored nothing (rc6's
# and the rc9 full stage's); and the image is put back however the row
# ends, where the rc9 driver had to restore it by hand.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

# dr3_tart runs tart in dockhand's Tart home.
dr3_tart() { TART_HOME="${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" tart "$@"; }

# dr3_has says whether dockhand's Tart home has the image.
dr3_has() { dr3_tart list --quiet 2>/dev/null | grep -qxF "$1"; }

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
	DR3_IMAGE=$(dr3_tart list --quiet 2>/dev/null | grep '^dockhand-base-' | head -1)
	[ -n "$DR3_IMAGE" ] || { row_fail "no dockhand image to delete"; return 0; }
	DR3_RELEASE=${DR3_IMAGE#dockhand-base-}
	dr3_has "dockhand-golden-$DR3_RELEASE" || { row_result "not run" "$DR3_IMAGE has no golden copy, dockhand-golden-$DR3_RELEASE, to restore it from"; DR3_IMAGE=""; return 0; }
	printf '%s\n' "$DR3_IMAGE" >"$ROW_DIR/image"
	allow_change "*"
	dr3_tart delete "$DR3_IMAGE"
	dh_json check -b "$DR3_BRANCH" || :
	local start=$SECONDS
	dh setup tart "$DR3_RELEASE" </dev/null || :
	printf '%s\n' "$((SECONDS - start))" >"$ROW_DIR/restore.seconds"
}
assert() {
	[ -n "${DR3_IMAGE:-}" ] || return 0
	jq -r '.error // ""' "$ROW_DIR/json/1.json" | grep -q "dockhand setup tart $DR3_RELEASE" ||
		{ row_fail "check didn't say what restores the image: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"; return; }
	grep -q "Restoring $DR3_IMAGE from dockhand-golden-$DR3_RELEASE" "$ROW_DIR/out.log" ||
		{ row_fail "setup tart didn't restore $DR3_IMAGE from its golden copy"; return; }
	dr3_has "$DR3_IMAGE" || { row_fail "$DR3_IMAGE isn't there after setup tart"; return; }
	row_pass "check named setup tart, which restored $DR3_IMAGE from its golden copy in $(cat "$ROW_DIR/restore.seconds")s"
}
# The image is there after, however the row ended.
teardown() {
	local image
	image=$(cat "$ROW_DIR/image" 2>/dev/null)
	[ -n "$image" ] && ! dr3_has "$image" && "$DH_BIN" setup tart "${image#dockhand-base-}" >>"$ROW_DIR/out.log" 2>&1
	return 0
}
