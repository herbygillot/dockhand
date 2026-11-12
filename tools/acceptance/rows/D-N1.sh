# stages: quick full
# D-N1: offline. With every connection reset, outdated, update, and
# submit's preview each say they couldn't reach the network, and change
# nothing half-way (H1).
. "${ROW_LIB:?}/fault.sh"
act() {
	local proxy
	# The branches before, which earlier rows' archived ones keep refs of:
	# only one this row starts is a failure (the rc6 full stage).
	git -C "$MACPORTS_TREE" branch --list 'dockhand/*' >"$ROW_DIR/git-branches.before"
	proxy=$(fault_proxy reset) || return 1
	(
		# An inherited NO_PROXY could let a host past the fault (#232).
		unset NO_PROXY no_proxy
		export HTTPS_PROXY="$proxy" HTTP_PROXY="$proxy" https_proxy="$proxy" http_proxy="$proxy"
		dh_json outdated "${ACCEPT_GO_PORT:?}" || :
		dh_json update "${ACCEPT_GO_PORT}" --new || :
	)
	DN1_ASKED=$(fault_proxy_asked)
	fault_proxy_stop
}
assert() {
	# A reset nothing went through tested nothing (#232).
	[ "${DN1_ASKED:-0}" -gt 0 ] || { row_result "not exercised" "no request reached the resetting proxy"; return; }
	local file err bad=""
	for file in "$ROW_DIR"/json/[0-9]*.json; do
		[ "$(cat "$file.exit")" = 0 ] && { bad="$bad $(cat "$file.args"): exit 0 offline"; continue; }
		err=$(jq -r '.error // ""' "$file")$(jq -r '[.result.ports[]?.problem // empty] | join(" ")' "$file" 2>/dev/null)
		# Words of a network that couldn't be reached, not any error that
		# names a proxy (#232).
		printf '%s' "$err" | matches -iE 'reach|connect|network|offline|reset|EOF' || bad="$bad $(cat "$file.args"): [$err]"
	done
	if [ -n "$bad" ]; then
		row_fail "offline, not said so:$bad"
	elif [ -n "$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*' | grep -vxF -f "$ROW_DIR/git-branches.before")" ]; then
		row_fail "a branch was started offline: $(git -C "$MACPORTS_TREE" branch --list 'dockhand/*' | grep -vxF -f "$ROW_DIR/git-branches.before" | tr '\n' ' ')"
	else
		row_pass "each said it couldn't reach the network, and nothing was started"
	fi
}
