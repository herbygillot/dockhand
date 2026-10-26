# stages: quick full
# B11: check.baseline in every check's path (v0.6.0, Then's item 4;
# c22615e7). With check.baseline on, a check -d of a deliberately broken
# local edit of the small Go port fails in its background drain, which
# is serve's loop, and the drain then runs its baseline, where only the
# foreground check ran one; the baseline is the check's, as its
# baseline_of says. A baseline standing across a rebase needs master to
# move, which the stage's pinned master doesn't: the engine's
# TestABaselineStandsWhereNothingItReadChanged covers it.
. "${ROW_LIB:?}/fault.sh"
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
		# The drain queues the baseline once the check is recorded failed,
		# planning it first, which takes seconds: the row asked straight
		# after wait returned, too early (the M1's run on rc1).
		local tries=0
		while [ "$tries" -lt 36 ]; do
			B11_BASELINE=$(stage_db "SELECT number FROM runs WHERE baseline_of = (SELECT id FROM runs WHERE number = ${B11_RUN#check-})" | head -1)
			[ -z "$B11_BASELINE" ] || break
			sleep 5
			tries=$((tries + 1))
		done
		[ -z "$B11_BASELINE" ] || dh_json wait "check-$B11_BASELINE" || :
		b11_drain_ended 120 || :
	fi
	export DOCKHAND_CONFIG="$stage"
}

# b11_drain is the pid of the background drain check -d started on the
# stage's database, if it's still running.
b11_drain() { pgrep -f "serve --drain.*--db ${DOCKHAND_DB:-$HOME/.dockhand/dockhand.db}" 2>/dev/null | head -1; }

# b11_drain_ended waits up to some seconds for the drain to end by itself,
# its queue empty.
b11_drain_ended() {
	local waited=0
	while [ -n "$(b11_drain)" ] && [ "$waited" -lt "$1" ]; do
		sleep 5
		waited=$((waited + 5))
	done
	[ -z "$(b11_drain)" ]
}

# teardown ends a drain the row's check -d left, before the stage's reset
# removes the worktree it would queue against: rc1's outlived the row,
# retried its baseline against a removed worktree, and C3's harm sweep read
# its VM as left running (the M1's run on rc1). Any check the row left
# queued or running is cancelled, and a VM still running is said.
teardown() {
	local pid run
	pid=$(b11_drain)
	if [ -n "$pid" ]; then
		kill "$pid" 2>/dev/null || :
		b11_drain_ended 60 || kill -KILL "$pid" 2>/dev/null || :
		printf 'teardown: ended the background drain (pid %s)\n' "$pid" >>"$ROW_DIR/notes"
	fi
	for run in $(stage_db "SELECT number FROM runs WHERE state IN ('queued','running')" 2>/dev/null); do
		"$DH_BIN" cancel "check-$run" >>"$ROW_DIR/out.log" 2>&1 || :
	done
	if command -v tart >/dev/null && fault_tart list 2>/dev/null | matches 'dockhand-check.*running'; then
		printf 'teardown: a check VM is still running: %s\n' "$(fault_tart list | grep 'dockhand-check.*running' | awk '{print $2}' | tr '\n' ' ')" >>"$ROW_DIR/notes"
	fi
}

assert() {
	[ -n "${B11_RUN:-}" ] || { row_fail "check -d queued no run"; return; }
	if [ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" != 2 ]; then
		row_fail "the broken check $B11_RUN exited $(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null), not 2"
		return
	fi
	if [ -z "${B11_BASELINE:-}" ]; then
		row_fail "check.baseline queued no baseline in the three minutes after $B11_RUN failed in the background"
		return
	fi
	if [ "$(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null)" != 0 ] && [ "$(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null)" != 2 ]; then
		row_fail "the baseline check-$B11_BASELINE didn't finish: $(jq -r '.error // empty' "$ROW_DIR/json/3.json")"
		return
	fi
	row_pass "$B11_RUN failed in its drain, which then ran its baseline, check-$B11_BASELINE"
}
