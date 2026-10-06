# stages: quick full
# C4: a Go module that moved, pomo's GitHub-to-Codeberg move, replayed at
# the pin, which has it on GitHub: the plan names both paths and writes
# go.vendors again, or refuses well. --plan only.
#
# The full stage's master is MacPorts' own, where pomo has since moved on,
# so its update had nothing to change and the case never ran (the rc6
# full stage): the row plans it against a master of its own at the quick
# stage's pin, a bare repository borrowing the ports clone's objects, as
# quick.sh makes, named by DOCKHAND_UPSTREAM for the row alone.
. "${ROW_LIB:?}/plan.sh"
setup() {
	[ "${ACCEPT_STAGE:-}" = full ] || return 0
	local pin=${ACCEPT_PIN:-026b3878a258b85619633709d89871f2f8750b94} objects
	if ! git -C "${MACPORTS_TREE:?}" cat-file -e "$pin^{commit}" 2>/dev/null; then
		row_result "not run" "the ports clone hasn't the quick stage's pin, $pin, where pomo is on GitHub"
		return 0
	fi
	objects=$(git -C "$MACPORTS_TREE" rev-parse --path-format=absolute --git-common-dir)/objects
	git init -q --bare -b master "$ROW_DIR/upstream.git" || return 1
	printf '%s\n' "$objects" >"$ROW_DIR/upstream.git/objects/info/alternates"
	git -C "$ROW_DIR/upstream.git" update-ref refs/heads/master "$pin" || return 1
	export DOCKHAND_UPSTREAM="$ROW_DIR/upstream.git"
}
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
