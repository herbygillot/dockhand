# stages: full
# A12: an Xcode image. setup tart --xcode makes this Mac's release's
# Xcode image from the Xcode a person staged in the host's xcode folder,
# and a check of a port that needs Xcode builds there, where without one
# it's reported as not built. One release's Xcode is enough to show it;
# the others' images are made the same way. With no .xip staged, or no
# ACCEPT_XCODE_PORT naming a port that needs Xcode, the row isn't run.
act() {
	host_only "this Mac's release's Xcode image" || return 0
	local xcode=${ACCEPT_HOST_ROOT:-/Users/Shared/dockhand-acceptance}/xcode
	if ! ls "$xcode"/*.xip >/dev/null 2>&1; then
		row_result "not run" "no Xcode .xip staged in $xcode, so no Xcode image is made"
		return 0
	fi
	if [ -z "${ACCEPT_XCODE_PORT:-}" ]; then
		row_result "not run" "set ACCEPT_XCODE_PORT to a small port whose Portfile sets use_xcode yes"
		return 0
	fi
	dh providers setup tart --xcode "$xcode" || :
	local branch
	branch=$(run_name a12-xcode)
	dh_setup revbump "$ACCEPT_XCODE_PORT" --branch "$branch" --subject "rebuild to check an Xcode image" || return 0
	dh_json check -b "$branch" </dev/null || :
}

assert() {
	local file=$ROW_DIR/json/1.json
	[ -f "$file.exit" ] || return 0
	if ! grep -qE '^(Made|Ready: ) ?dockhand-xcode-[^ ]*:? .*with Xcode ' "$ROW_DIR/out.log"; then
		row_fail "setup tart --xcode made no Xcode image: $(grep -iE 'xcode' "$ROW_DIR/out.log" | tail -2 | tr '\n' ';')"
		return
	fi
	if [ "$(cat "$file.exit")" != 0 ]; then
		row_fail "the check of $ACCEPT_XCODE_PORT on the Xcode image exited $(cat "$file.exit"): $(jq -r '.error // ""' "$file")"
		return
	fi
	if jq -e '[.result.targets[]? | select(.passed != true)] | length > 0' "$file" >/dev/null 2>&1; then
		row_fail "the check reported $ACCEPT_XCODE_PORT not built, as with no Xcode image"
		return
	fi
	row_pass "made the Xcode image and built $ACCEPT_XCODE_PORT in it"
}
