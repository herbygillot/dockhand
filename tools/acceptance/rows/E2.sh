# stages: quick full
# E2: an obsolete stub following its replacement: terraform's stub moves
# with terraform-1.16's update, with --with-obsolete. --plan only.
. "${ROW_LIB:?}/plan.sh"
act() { E2_PLAN=$(plan_update terraform-1.16 --with-obsolete); }
assert() {
	plan_ok "$E2_PLAN" || { row_fail "the plan crashed: $(cat "$E2_PLAN.exit")"; return; }
	if [ "$(cat "$E2_PLAN.exit")" = 1 ]; then
		row_refused_well "$(jq -r .error "$E2_PLAN")"
	else
		row_pass "planned, the stub with it: $(grep -c 'obsolete' "$ROW_DIR/out.log") lines name it"
	fi
}
