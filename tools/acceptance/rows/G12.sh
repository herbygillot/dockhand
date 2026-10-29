# stages: full
# G12: a GitHub check per release (v0.7.0). check --on github of the small
# Go port plans an environment for each macOS release MacPorts' workflow
# at master names, and records each release's job as its own result for
# the port, not one result standing for the matrix; github:15 is refused,
# naming tart and local, since the workflow runs its whole matrix.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

act() {
	host_only "a fork with Actions" || return 0
	allow_push "*dockhand-check/*"
	dh_setup update "$(port)" --new || return 0
	G12_BRANCH=$(own_branch)
	dh check -b "$G12_BRANCH" --on github:15 --plan || :
	dh_json check -b "$G12_BRANCH" --on github || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G12_BRANCH:-}" ] || return 0
	sed -n '/^\$ dockhand check .*--on github:15/,/^\[exit/p' "$ROW_DIR/out.log" |
		matches -F "the github provider builds every release MacPorts' workflow names; to check one release, use tart or local" ||
		{ row_fail "github:15 wasn't refused naming tart and local: $(sed -n '/^\$ dockhand check .*--on github:15/,/^\[exit/p' "$ROW_DIR/out.log" | tail -2 | tr '\n' ' ')"; return; }
	local check=$ROW_DIR/json/1.json environments recorded
	environments=$(jq '[.result.plan.environments[]? | select(.provider == "github")] | length' "$check" 2>/dev/null || echo 0)
	[ "${environments:-0}" -ge 2 ] || { row_fail "check --on github planned $environments GitHub environments, not one for each release of the workflow's matrix: $(jq -c '.result.plan.environments' "$check" 2>/dev/null)"; return; }
	# Each release's result is its own: as many results for the port as
	# environments, each built (passed or failed), or not run with why.
	recorded=$(jq --arg port "$(port)" '[.result.targets[]? | select(.name == $port) | .results[]?] | length' "$check" 2>/dev/null || echo 0)
	[ "$recorded" = "$environments" ] || { row_fail "the port has $recorded results, not one for each of $environments releases: $(jq -c --arg port "$(port)" '.result.targets[]? | select(.name == $port) | .results' "$check" 2>/dev/null)"; return; }
	if [ "$(cat "$check.exit" 2>/dev/null)" = 0 ]; then
		row_pass "check --on github planned $environments releases and recorded $(port)'s job on each as its own result; github:15 was refused, naming tart and local"
	else
		judged "check --on github recorded $(port) on each of $environments releases as its own result, and exited $(cat "$check.exit"): a release's job failed or never ran, which its result says"
	fi
}
