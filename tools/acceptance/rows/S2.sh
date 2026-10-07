# stages: full
# S2: outdated --mine over a maintainer's ports, some 320 of
# ACCEPT_MAINTAINER's, then update --outdated across 30 or more of them
# with --check: paced under GitHub's limits, the disk above
# cleanup.min_free, one branch and one check each.
#
# outdated has no whole-tree mode: --all lists every port of those it
# looks at, and with no ports named and no --mine it names nothing to look
# at, and was refused in rc6's run 4 and rc8's (the rc8 full stage). The
# maintainer line goes into dockhand's config for the row, as A10's does,
# and comes back out in teardown.
setup() {
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/config.before"
	{ printf 'maintainer = "%s"\n' "${ACCEPT_MAINTAINER:-@herbygillot}"; sed '/^maintainer *=/d' "$DOCKHAND_CONFIG"; } >"$ROW_DIR/config.toml" && cp "$ROW_DIR/config.toml" "$DOCKHAND_CONFIG"
}
teardown() {
	[ -f "$ROW_DIR/config.before" ] && cp "$ROW_DIR/config.before" "$DOCKHAND_CONFIG"
	return 0
}
act() {
	host_only "a maintainer's ports against GitHub" || return 0
	allow_change "*"
	# It exits 3 where some couldn't be checked, which is no reason to stop.
	dh_json outdated --mine --all || :
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
