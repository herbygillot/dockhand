# stages: quick full
# E3: ${worksrcpath} in configure.cmd, qemu: a correct plan or a clean
# refusal. --plan only.
. "${ROW_LIB:?}/plan.sh"
act() { E3_PLAN=$(plan_update qemu); }
assert() {
	plan_ok "$E3_PLAN" || { row_fail "the plan crashed: $(cat "$E3_PLAN.exit")"; return; }
	[ "$(cat "$E3_PLAN.exit")" = 1 ] && row_refused_well "$(jq -r .error "$E3_PLAN")" || row_pass "planned"
}
