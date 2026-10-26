# stages: quick full
# C11: a Go port that lists go.vendors plans, on git-credential-oauth,
# which the pin has at 0.17.2. golang 1.0 appends each vendored archive's
# file, sums, and size as declarations of their own, and sets
# extract.rename and extract.cmd where go.vendors lists any. v0.4.0
# planned these ports, and v0.5.0-rc4 refused every one: "the checksums
# are set by the golang 1.0 PortGroup" (field testing, 2026-10-23). Any
# refusal fails. --plan only.
. "${ROW_LIB:?}/plan.sh"
port() { printf '%s' git-credential-oauth; }
act() { C11_PLAN=$(plan_update "$(port)"); }
assert() {
	if ! plan_ok "$C11_PLAN"; then
		row_fail "$(port)'s plan exited $(cat "$C11_PLAN.exit"): $(jq -r '.error // empty' "$C11_PLAN")"
	elif [ "$(cat "$C11_PLAN.exit")" != 0 ]; then
		row_fail "$(port), which lists go.vendors, was refused, where v0.4.0 planned it: $(jq -r '.error // empty' "$C11_PLAN")"
	elif ! plan_words "$(port)" | matches -E '^\+go\.setup .*/git-credential-oauth [0-9]'; then
		row_fail "$(port)'s plan didn't move its version: $(plan_words "$(port)" | grep -E '^[-+]go\.setup' | tr '\n' ';')"
	else
		row_pass "planned: $(plan_words "$(port)" | grep -E '^\+go\.setup' | head -1 | awk '{print ($NF == "v" ? $(NF-1) : $NF)}')"
	fi
}
