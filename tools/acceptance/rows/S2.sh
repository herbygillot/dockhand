# stages: full
# S2: outdated --mine over a maintainer's ports, some 320 of
# ACCEPT_MAINTAINER's, then ship --to check across 30 or more of them: paced under GitHub's limits, the disk above
# cleanup.min_free, one branch and one check each.
#
# outdated has no whole-tree mode: --current lists every port of those it
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
	dh_json outdated --mine --current || :
	jq -r '[.result.ports[]? | select(.outdated) | .port][:30] | .[]' "$DH_LAST_JSON" >"$ROW_DIR/ports"
	# shellcheck disable=SC2046
	dh_json ship $(cat "$ROW_DIR/ports") --to check -y || :
	dh serve --drain || :
}
# Each port asked for is graded as prepared, in changes, or stopped with a
# reason, in skipped: a maintainer's ports include ones another pull request
# is open for, or a newer major holds, which stop rightly, and a count of
# 30 prepared failed the rc1 full stage on those (S2).
assert() {
	local asked prepared stopped unexplained
	asked=$(grep -c . "$ROW_DIR/ports" 2>/dev/null || echo 0)
	[ "$asked" -gt 0 ] || { row_fail "outdated --mine --current found no outdated port to ship"; return; }
	prepared=$(jq -r '[.result.changes[]?] | length' "$ROW_DIR/json/2.json" 2>/dev/null || echo 0)
	stopped=$(jq -r '[.result.skipped[]? | select((.reason // "") != "")] | length' "$ROW_DIR/json/2.json" 2>/dev/null || echo 0)
	unexplained=$(jq -r --rawfile asked "$ROW_DIR/ports" '($asked | split("\n") | map(select(. != ""))) - [.result.changes[]?.port, (.result.skipped[]? | select((.reason // "") != "") | .port)] | join(" ")' "$ROW_DIR/json/2.json" 2>/dev/null)
	[ -z "$unexplained" ] || { row_fail "of $asked ports, these were neither prepared nor stopped with a reason: $unexplained"; return; }
	judged "of $asked ports, $prepared were prepared and $stopped stopped with a reason; it stayed under GitHub's limits, the disk stayed above cleanup.min_free, and each prepared port had one branch and one check"
}
