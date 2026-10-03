# stages: quick full
# B3: the batch, on the quick stage's two ports: update --outdated with
# --check, serve --drain to run the checks, and each branch's submit
# preview. Each port gets its own branch and check, and the batch's output
# and exit code say what worked.
act() {
	dh_json update --outdated "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}" --check -y || :
	dh serve --drain || :
	local branch
	for branch in $(jq -r '.result.prepared[]? | select(.tidied) | .branch' "$ROW_DIR/json/1.json" 2>/dev/null); do
		dh_json submit -b "$branch" --plan || :
	done
}

assert() {
	local batch=$ROW_DIR/json/1.json n
	n=$(jq -r '[.result.prepared[]? | select(.tidied)] | length' "$batch" 2>/dev/null || echo 0)
	case "$(cat "$batch.exit")" in
	0) ;;
	3) row_known "the batch needs a look: $(jq -r '[.result.prepared[]? | select(.problem) | .problem] | join("; ")' "$batch")"; return ;;
	*) row_fail "the batch did nothing that worked, exit $(cat "$batch.exit"): $(jq -r '.error // empty' "$batch")"; return ;;
	esac
	if [ "$n" -lt 2 ]; then
		row_fail "expected a branch for each of the two ports, got $n"
		return
	fi
	if grep -q '^\[exit [^0]' <(sed -n '/^\$ dockhand serve --drain/,/^\[exit/p' "$ROW_DIR/out.log"); then
		row_fail "serve --drain failed: $(sed -n '/^\$ dockhand serve --drain/,/^\[exit/p' "$ROW_DIR/out.log" | tail -3 | tr '\n' ';')"
		return
	fi
	row_pass "two branches, one commit and one check each, drained, and previewed"
}
