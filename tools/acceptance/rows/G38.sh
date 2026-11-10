# stages: quick full
# G38: cleaning a branch removes the clones its checks kept (field
# testing's note 13). A Tart check of the day's Go port with --keep keeps
# its clone; the branch archived, clean --delete <branch> --plan names
# the clone, kept by its check for dockhand shell, and clean --delete
# <branch> --yes removes it, tart list no longer having it, before the
# record goes, where the record went and the clone stayed for good.
# shellcheck source=../lib/fault.sh
. "${ROW_LIB:?}/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a Tart check" || return 0
	dh_setup update "$(port)" --new || return 0
	G38_BRANCH=$(own_branch)
	dh_json check -b "$G38_BRANCH" --keep </dev/null || :
	G38_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json" 2>/dev/null)
	# The clone is named for the run, its ID's _ a -.
	G38_CLONE=$(fault_tart list --format json 2>/dev/null | jq -r --arg run "$(jq -r '.result.run.id // empty' "$ROW_DIR/json/1.json" 2>/dev/null | tr _ -)" '.[] | select(.Name | contains("dockhand-check-" + $run + "-")) | .Name' | head -1)
	dh_setup archive "$G38_BRANCH" </dev/null || return 0
	allow_ref_gone "*/$G38_BRANCH"
	dh_json clean --delete "$G38_BRANCH" --plan </dev/null || :
	dh_json clean --delete "$G38_BRANCH" --yes </dev/null || :
}
assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G38_CLONE:-}" ] || { row_fail "${G38_RUN:-the check} kept no clone tart lists"; return; }
	local planned=$ROW_DIR/json/2.json forgot=$ROW_DIR/json/3.json
	jq -e --arg clone "$G38_CLONE" --arg branch "$G38_BRANCH" '.result.branches[$branch].leftovers[]? | select(.ref == $clone)' "$planned" >/dev/null ||
		{ row_fail "clean --delete --plan didn't list $G38_CLONE: $(jq -c '.result.branches // empty' "$planned" 2>/dev/null)"; return; }
	grep -q "remove Tart clone $G38_CLONE, kept by ${G38_RUN:-check} for dockhand shell" "$ROW_DIR/out.log" ||
		{ row_fail "the plan didn't name the clone as kept by ${G38_RUN:-its check}"; return; }
	[ "$(cat "$forgot.exit" 2>/dev/null)" = 0 ] || { row_fail "clean --delete exited $(cat "$forgot.exit" 2>/dev/null): $(jq -r '.error // empty' "$forgot")"; return; }
	if fault_tart list --format json 2>/dev/null | jq -e --arg clone "$G38_CLONE" '.[] | select(.Name == $clone)' >/dev/null; then
		row_fail "$G38_CLONE is still in tart list after clean --delete"
		return
	fi
	row_pass "clean --delete planned $G38_CLONE, kept by $G38_RUN, and removed it before forgetting $G38_BRANCH"
}
