# stages: quick full
# E6: a Python port, py-requests: a correct plan or a clean refusal.
# --plan only.
. "${ROW_LIB:?}/plan.sh"
act() { E6_PLAN=$(plan_update py-requests); }
assert() {
	plan_ok "$E6_PLAN" || { row_fail "the plan crashed: $(cat "$E6_PLAN.exit")"; return; }
	[ "$(cat "$E6_PLAN.exit")" = 1 ] && row_refused_well "$(jq -r .error "$E6_PLAN")" || row_pass "planned"
}
