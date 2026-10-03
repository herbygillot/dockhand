# stages: full
# S2: outdated --all over the whole tree once, then update --outdated
# across 30 or more ports with --check: paced under GitHub's limits, the
# disk above cleanup.min_free, one branch and one check each.
act() {
	host_only "the whole tree against GitHub" || return 0
	allow_change "*"
	dh_json outdated --all || return 0
	jq -r '[.result.ports[]? | select(.outdated) | .port][:30] | .[]' "$DH_LAST_JSON" >"$ROW_DIR/ports"
	# shellcheck disable=SC2046
	dh_json update --outdated $(cat "$ROW_DIR/ports") --check -y || :
	dh serve --drain || :
}
assert() {
	local n
	n=$(jq -r '[.result.prepared[]?] | length' "$ROW_DIR/json/2.json" 2>/dev/null || echo 0)
	[ "$n" -ge 30 ] || { row_fail "only $n ports were prepared"; return; }
	judged "it stayed under GitHub's limits, the disk stayed above cleanup.min_free, and each port had one branch and one check"
}
