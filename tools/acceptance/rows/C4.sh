# stages: quick full
# C4: a Go module that moved, pomo's GitHub-to-Codeberg move, replayed at
# the pin, which has it on GitHub: the plan names both paths and writes
# go.vendors again, or refuses well. --plan only.
. "${ROW_LIB:?}/plan.sh"
act() { C4_PLAN=$(plan_update pomo); }
assert() {
	if ! plan_ok "$C4_PLAN"; then
		row_fail "pomo's plan exited $(cat "$C4_PLAN.exit"): $(jq -r '.error // empty' "$C4_PLAN")"
	elif [ "$(cat "$C4_PLAN.exit")" = 1 ]; then
		row_refused_well "$(jq -r .error "$C4_PLAN")"
	elif plan_words pomo | grep -qi codeberg && plan_words pomo | grep -q 'go.vendors'; then
		row_pass "both module paths named, and go.vendors written again"
	else
		row_known "pomo planned without naming the move: $(plan_words pomo | grep -v '^[-+ @]' | head -3 | tr '\n' ';')"
	fi
}
