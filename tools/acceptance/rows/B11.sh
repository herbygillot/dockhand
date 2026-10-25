# stages: quick full
# B11: check.baseline in every check's path (v0.6.0, Then's item 4;
# c22615e7). With check.baseline on, a check -d of a deliberately broken
# local edit of the small Go port fails in its background drain, which
# is serve's loop, and the drain then runs its baseline, where only the
# foreground check ran one; the baseline is the check's, as its
# baseline_of says. A baseline standing across a rebase needs master to
# move, which the stage's pinned master doesn't: the engine's
# TestABaselineStandsWhereNothingItReadChanged covers it.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

# b11_config is the stage's configuration with check.baseline on, in the
# [check] table it has, or one of its own.
b11_config() {
	if grep -q '^\[check\]' "${DOCKHAND_CONFIG:?}"; then
		sed '/^\[check\]/a\
baseline = true
' "$DOCKHAND_CONFIG" | awk '!(/^baseline *=/ && seen++)' >"$ROW_DIR/b11.toml"
	else
		{ cat "$DOCKHAND_CONFIG"; printf '\n[check]\nbaseline = true\n'; } >"$ROW_DIR/b11.toml"
	fi
}

act() {
	B11_BRANCH=$(run_name b11-baseline)
	dh_setup start "$B11_BRANCH" --port "$(port)" || return 0
	local dir portfile
	dir=$("$DH_BIN" path "$B11_BRANCH") || return 0
	portfile=$(find "$dir" -path "*/$(port)/Portfile" | head -1)
	printf '\npost-destroot {\n    return -code error "acceptance: a deliberate failure"\n}\n' >>"$portfile"
	allow_change "$portfile"
	b11_config
	# The row's configuration for its commands alone; the stage's is put
	# back for the harm sweep.
	local stage=$DOCKHAND_CONFIG
	export DOCKHAND_CONFIG="$ROW_DIR/b11.toml"
	dh_json check -b "$B11_BRANCH" -d || :
	B11_RUN=$(jq -r '.result.run.name // empty' "$DH_LAST_JSON")
	if [ -n "$B11_RUN" ]; then
		dh_json wait "$B11_RUN" || :
		B11_BASELINE=$(stage_db "SELECT number FROM runs WHERE baseline_of = (SELECT id FROM runs WHERE number = ${B11_RUN#check-})" | head -1)
		[ -z "$B11_BASELINE" ] || dh_json wait "check-$B11_BASELINE" || :
	fi
	export DOCKHAND_CONFIG="$stage"
}

assert() {
	[ -n "${B11_RUN:-}" ] || { row_fail "check -d queued no run"; return; }
	if [ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" != 2 ]; then
		row_fail "the broken check $B11_RUN exited $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null), not 2"
		return
	fi
	if [ -z "${B11_BASELINE:-}" ]; then
		row_fail "check.baseline queued no baseline after $B11_RUN failed in the background"
		return
	fi
	if [ "$(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null)" != 0 ] && [ "$(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null)" != 2 ]; then
		row_fail "the baseline check-$B11_BASELINE didn't finish: $(jq -r '.error // empty' "$ROW_DIR/json/3.json")"
		return
	fi
	row_pass "$B11_RUN failed in its drain, which then ran its baseline, check-$B11_BASELINE"
}
