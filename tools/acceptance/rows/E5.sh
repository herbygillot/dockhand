# stages: quick full
# E5: a Git-fetched port, the first fetch.type git one at the pin: a
# correct plan or a clean refusal. --plan only.
. "${ROW_LIB:?}/plan.sh"
act() {
	E5_PORT=$(git -C "$MACPORTS_TREE" grep -l '^fetch.type[[:space:]]*git' HEAD -- '*/Portfile' | head -1 | awk -F/ '{print $(NF-1)}')
	[ -n "$E5_PORT" ] && E5_PLAN=$(plan_update "$E5_PORT")
}
assert() {
	[ -n "${E5_PORT:-}" ] || { row_known "no Git-fetched port at the pin"; return; }
	plan_ok "$E5_PLAN" || { row_fail "$E5_PORT's plan crashed: $(cat "$E5_PLAN.exit")"; return; }
	[ "$(cat "$E5_PLAN.exit")" = 1 ] && row_refused_well "$E5_PORT: $(jq -r .error "$E5_PLAN")" || row_pass "$E5_PORT planned"
}
