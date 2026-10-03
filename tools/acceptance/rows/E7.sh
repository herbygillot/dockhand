# stages: quick full
# E7: a C library with --revbump-dependents, libuv. --plan only.
. "${ROW_LIB:?}/plan.sh"
act() { E7_PLAN=$(plan_update libuv --revbump-dependents); }
assert() {
	plan_ok "$E7_PLAN" || { row_fail "the plan crashed: $(cat "$E7_PLAN.exit")"; return; }
	[ "$(cat "$E7_PLAN.exit")" = 1 ] && row_refused_well "$(jq -r .error "$E7_PLAN")" || row_pass "planned, with its dependents"
}
