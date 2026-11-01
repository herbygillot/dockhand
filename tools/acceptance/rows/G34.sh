# stages: full
# G34: a check's wait for its slot is said (the check-execution service's
# first step, c69cee54). After a check of the small Go port's update has
# run, logs <check> says "Queued <time>, waited <took> for its slot,
# finished <time>." under its state, and logs --json's run carries
# started_at and waited_seconds. It needs a check that takes a slot and
# runs, so a VM: the quick stage run without an image can't show it.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a check that runs in a VM" || return 0
	dh_setup update "$(port)" --new || return 0
	G34_BRANCH=$(own_branch)
	[ -n "$G34_BRANCH" ] || return 0
	dh_json check -b "$G34_BRANCH" </dev/null || :
	G34_RUN=$(jq -r '.result.run.name // empty' "$DH_LAST_JSON")
	[ -n "$G34_RUN" ] || return 0
	dh logs "$G34_RUN" </dev/null || :
	dh_json logs "$G34_RUN" </dev/null || :
}
assert() {
	[ -n "${G34_RUN:-}" ] || { row_fail "the check named no run: $(jq -r '.error // empty' "$ROW_DIR/json/1.json" 2>/dev/null)"; return; }
	if ! sed -n "/^\\\$ dockhand logs $G34_RUN\$/,/^\\[exit/p" "$ROW_DIR/out.log" | matches -E '^  Queued .*, waited .* for its slot'; then
		row_fail "logs $G34_RUN didn't say its wait for its slot: $(sed -n "/^\\\$ dockhand logs $G34_RUN\$/,/^\\[exit/p" "$ROW_DIR/out.log" | sed -n 2,3p | tr '\n' ' ')"
		return
	fi
	if [ -z "$(jq -r '.. | objects | select(has("waited_seconds")) | .started_at // empty' "$DH_LAST_JSON" | head -1)" ]; then
		row_fail "logs --json $G34_RUN carried no started_at and waited_seconds"
		return
	fi
	row_pass "$(sed -n "/^\\\$ dockhand logs $G34_RUN\$/,/^\\[exit/p" "$ROW_DIR/out.log" | grep -E '^  Queued ' | head -1 | sed 's/^ *//')"
}
