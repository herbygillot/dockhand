# stages: quick full
# G5: several ports of one Portfile, one branch and a commit each (field
# testing's four kubectl subports): update names the day's two outdated
# subports of one Portfile, ACCEPT_SUBPORTS ("kubectl-1.34 kubectl-1.35"),
# and starts one branch, named for their directory; tidy -y then commits
# each update on its own, titled for its port, in the order made. A
# selection's grouping, update --mine's and ship's, is engine.PlanOutdated's,
# which its tests hold. Without ACCEPT_SUBPORTS the row isn't run.
act() {
	[ -n "${ACCEPT_SUBPORTS:-}" ] || { row_result "not run" "ACCEPT_SUBPORTS names no two outdated subports of one Portfile"; return 0; }
	# shellcheck disable=SC2086
	dh_setup update $ACCEPT_SUBPORTS --new || return 0
	dh_json tidy -b "$(own_branch)" -y </dev/null || :
	"$DH_BIN" --json status 2>/dev/null | jq -r '.result.branches[]?.name' 2>/dev/null |
		grep -vxF -f "$ROW_DIR/branches.before" >"$ROW_DIR/branches.new" || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	local branch dir port subjects
	[ "$(grep -c . "$ROW_DIR/branches.new")" = 1 ] || { row_fail "update of $ACCEPT_SUBPORTS started $(grep -c . "$ROW_DIR/branches.new") branches: $(tr '\n' ' ' <"$ROW_DIR/branches.new")"; return; }
	branch=$(cat "$ROW_DIR/branches.new")
	[ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ] || { row_fail "tidy -y of $branch exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)"; return; }
	dir=$("$DH_BIN" path "$branch")
	# shellcheck disable=SC2086
	set -- $ACCEPT_SUBPORTS
	subjects=$(git -C "$dir" log --format=%s -n "$#" | tail -r 2>/dev/null || git -C "$dir" log --format=%s -n "$#" | tac)
	for port in "$@"; do
		printf '%s\n' "$subjects" | matches -E "^$port: update to " || { row_fail "no commit of its own titled for $port: $(printf '%s' "$subjects" | tr '\n' ';')"; return; }
	done
	[ "$(printf '%s\n' "$subjects" | head -1 | cut -d: -f1)" = "$1" ] || { row_fail "the commits aren't in the order made: $(printf '%s' "$subjects" | tr '\n' ';')"; return; }
	row_pass "one branch, $branch, with a commit each: $(printf '%s' "$subjects" | tr '\n' ';')"
}
