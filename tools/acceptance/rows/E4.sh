# stages: quick full
# E4: a non-GitHub forge, reposurgeon on GitLab: a correct plan or a clean
# refusal. --plan only. (pomo's Codeberg move is C4's.)
. "${ROW_LIB:?}/plan.sh"
act() { E4_PLAN=$(plan_update reposurgeon); }
assert() {
	plan_ok "$E4_PLAN" || { row_fail "the plan crashed: $(cat "$E4_PLAN.exit")"; return; }
	[ "$(cat "$E4_PLAN.exit")" = 1 ] && row_refused_well "$(jq -r .error "$E4_PLAN")" || row_pass "planned"
}
