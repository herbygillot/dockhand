# stages: quick full
# G38: cleaning a branch removes the clones its checks kept (field
# testing's note 13). A Tart check of the day's Go port with --fresh
# --keep keeps its clone (with --keep alone, rc4's full stage reused an
# earlier check's builds and kept none; #220). The branch is tidied, so
# archive takes its worktree, and archived; clean --delete <branch> --plan
# names the clone, kept by its check for dockhand shell, and clean --delete
# <branch> --yes removes it, tart list no longer having it, before the
# record goes, where the record went and the clone stayed for good.
# shellcheck source=../lib/fault.sh
. "${ROW_LIB:?}/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a Tart check" || return 0
	dh_setup update "$(port)" --new || return 0
	G38_BRANCH=$(own_branch)
	dh_json check -b "$G38_BRANCH" --fresh --keep </dev/null || :
	G38_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json" 2>/dev/null)
	# The clone is named for the run, its ID's _ a -.
	G38_CLONE=$(fault_tart list --format json 2>/dev/null | jq -r --arg run "$(jq -r '.result.run.id // empty' "$ROW_DIR/json/1.json" 2>/dev/null | tr _ -)" '.[] | select(.Name | contains("dockhand-check-" + $run + "-")) | .Name' | head -1)
	dh_setup tidy -b "$G38_BRANCH" -y </dev/null || return 0
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
	# The envelope's own fields: run with --json, clean writes no text line
	# to grep (rc4's reruns). The plan names the clone by its check, not yet
	# removed; kept is empty where the plan removes it (engine/clean.go),
	# so it isn't asked for (rc4's batch).
	jq -e --arg clone "$G38_CLONE" --arg branch "$G38_BRANCH" --arg run "${G38_RUN:-}" \
		'.result.branches[$branch].leftovers[]? | select(.ref == $clone and ($run == "" or .check == $run) and .removed == false)' "$planned" >/dev/null ||
		{ row_fail "the plan didn't name the clone as kept by ${G38_RUN:-its check}: $(jq -c --arg branch "$G38_BRANCH" '.result.branches[$branch].leftovers // empty' "$planned" 2>/dev/null)"; return; }
	[ "$(cat "$forgot.exit" 2>/dev/null)" = 0 ] || { row_fail "clean --delete exited $(cat "$forgot.exit" 2>/dev/null): $(jq -r '.error // empty' "$forgot")"; return; }
	jq -e --arg clone "$G38_CLONE" '[.. | objects | select(.ref? == $clone and .removed? == true)] | length > 0' "$forgot" >/dev/null ||
		say "clean --delete's envelope didn't mark $G38_CLONE removed; tart list decides"
	if fault_tart list --format json 2>/dev/null | jq -e --arg clone "$G38_CLONE" '.[] | select(.Name == $clone)' >/dev/null; then
		row_fail "$G38_CLONE is still in tart list after clean --delete"
		return
	fi
	row_pass "clean --delete planned $G38_CLONE, kept by $G38_RUN, and removed it before forgetting $G38_BRANCH"
}
