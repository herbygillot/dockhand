# stages: quick full
# C2: cargo overrides and pins, on pgdog, which pins rev-pinned crates by a
# label dockhand doesn't generate: its plan drops a pin the new lock passes,
# saying so, or refuses, naming the crates. --plan only. A refusal that
# names no crate or pin fails: v0.4.0 planned pgdog, and v0.5.0-rc1's
# "[pgdog.dist_subdir changed]" passed as refused well.
. "${ROW_LIB:?}/plan.sh"
act() { C2_PLAN=$(plan_update pgdog); }
assert() {
	if ! plan_ok "$C2_PLAN"; then
		row_fail "pgdog's plan exited $(cat "$C2_PLAN.exit"): $(jq -r '.error // empty' "$C2_PLAN")"
	elif [ "$(cat "$C2_PLAN.exit")" = 1 ] && jq -r '.error // empty' "$C2_PLAN" | grep -qiE 'crate|pin'; then
		row_refused_well "$(jq -r .error "$C2_PLAN")"
	elif [ "$(cat "$C2_PLAN.exit")" = 1 ]; then
		row_fail "pgdog was refused for what isn't its crates or pins, where v0.4.0 planned it: $(jq -r .error "$C2_PLAN")"
	elif plan_words pgdog | grep -q 'so the pin is dropped'; then
		row_pass "a pin the new lock passes is dropped, and said"
	else
		row_pass "planned, with no pin to drop"
	fi
}
