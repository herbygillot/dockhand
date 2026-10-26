# stages: quick full
# C12: a Go port whose module carries its major version plans, on direnv,
# which the pin has at 2.37.1: its go.mod's module is
# github.com/direnv/direnv/v2, its go.package github.com/direnv/direnv.
# Dependency regeneration compared the two whole and refused it as a
# module that moved (field testing, 2026-10-23, behind rc4's go.vendors
# refusal). Any refusal fails. --plan only.
. "${ROW_LIB:?}/plan.sh"
port() { printf '%s' direnv; }
act() { C12_PLAN=$(plan_update "$(port)"); }
assert() {
	if ! plan_ok "$C12_PLAN"; then
		row_fail "$(port)'s plan exited $(cat "$C12_PLAN.exit"): $(jq -r '.error // empty' "$C12_PLAN")"
	elif [ "$(cat "$C12_PLAN.exit")" != 0 ]; then
		row_fail "$(port), whose module is .../v2, was refused: $(jq -r '.error // empty' "$C12_PLAN")"
	elif plan_current "$(port)"; then
		plan_not_due "$(port)"
	elif ! plan_words "$(port)" | matches -E '^\+go\.setup .*/direnv [0-9]'; then
		row_fail "$(port)'s plan didn't move its version: $(plan_words "$(port)" | grep -E '^[-+]go\.setup' | tr '\n' ';')"
	else
		row_pass "planned: $(plan_words "$(port)" | grep -E '^\+go\.setup' | head -1 | awk '{print ($NF == "v" ? $(NF-1) : $NF)}')"
	fi
}
