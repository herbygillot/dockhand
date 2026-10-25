# stages: quick full
# D-N6: a read of GitHub asked again costs nothing where nothing moved
# (v0.6.0, Then's item 5; 0f6e9a39). outdated over the day's ports, twice
# in a row: the second spends a tenth of the first's requests or fewer,
# as each dockhand's line in DOCKHAND_GITHUB_LOG counts them, its reads
# answered on GitHub's 304s from the answers kept with their ETags.
d_n6_ports() { printf '%s %s jq' "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}"; }

act() {
	# A cache of the row's own, empty, so the first look is cold whatever
	# earlier rows read; the stage's is put back after.
	local stage=${DOCKHAND_GITHUB_CACHE:-}
	export DOCKHAND_GITHUB_CACHE="$ROW_DIR/github-cache"
	# shellcheck disable=SC2046
	dh outdated $(d_n6_ports) || :
	# shellcheck disable=SC2046
	dh outdated $(d_n6_ports) || :
	if [ -n "$stage" ]; then export DOCKHAND_GITHUB_CACHE="$stage"; else unset DOCKHAND_GITHUB_CACHE; fi
}

assert() {
	local log=${DOCKHAND_GITHUB_LOG:-$ROW_DIR/github.log} first second
	first=$(awk -F '\t' -v a="outdated $(d_n6_ports)" '$3 == a { print $1 }' "$log" 2>/dev/null | sed -n 1p)
	second=$(awk -F '\t' -v a="outdated $(d_n6_ports)" '$3 == a { print $1 }' "$log" 2>/dev/null | sed -n 2p)
	if [ -z "$first" ] || [ -z "$second" ]; then
		row_fail "the two looks' requests weren't logged in $log"
		return
	fi
	if [ "$first" -lt 5 ]; then
		row_result "not run" "the first look spent $first requests, too few to weigh a second against"
		return
	fi
	if [ "$((second * 10))" -gt "$first" ]; then
		row_fail "the second look spent $second requests, more than a tenth of the first's $first"
		return
	fi
	row_pass "the first look spent $first requests, the second $second"
}
