# stages: quick full
# C13: a Go port with a long go.vendors block plans, on opkssh, which the
# pin has at 0.16.0, about 500 lines of go.vendors. Every checksum its
# PortGroup appends had the block's whole command on its stack, sent once
# for each, and the evaluation's 18.5 MB went past the RPC frame limit
# (field testing, 2026-10-23). Any refusal fails. --plan only.
. "${ROW_LIB:?}/plan.sh"
port() { printf '%s' opkssh; }
act() { C13_PLAN=$(plan_update "$(port)"); }
assert() {
	if ! plan_ok "$C13_PLAN"; then
		row_fail "$(port)'s plan exited $(cat "$C13_PLAN.exit"): $(jq -r '.error // empty' "$C13_PLAN")"
	elif [ "$(cat "$C13_PLAN.exit")" != 0 ]; then
		row_fail "$(port), with a long go.vendors, was refused: $(jq -r '.error // empty' "$C13_PLAN")"
	elif ! plan_words "$(port)" | grep -qE '^\+go\.setup .*/opkssh [0-9]'; then
		row_fail "$(port)'s plan didn't move its version: $(plan_words "$(port)" | grep -E '^[-+]go\.setup' | tr '\n' ';')"
	else
		row_pass "planned: $(plan_words "$(port)" | grep -E '^\+go\.setup' | head -1 | awk '{print ($NF == "v" ? $(NF-1) : $NF)}')"
	fi
}
