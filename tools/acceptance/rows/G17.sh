# stages: full
# G17: an update moving github.tarball_from tarball to archive (v0.7.0).
# An update of ACCEPT_TARBALL_PORT, an outdated port that fetches GitHub's
# tarball, writes github.tarball_from archive, since its checksums are made
# anew, says so among what it fixed, and its check in Tart passes. Without
# the port, the row isn't run.
port() { printf '%s' "${ACCEPT_TARBALL_PORT:-}"; }

act() {
	host_only "an outdated port fetching GitHub's tarball, checked in Tart" || return 0
	if [ -z "$(port)" ]; then
		row_result "not run" "set ACCEPT_TARBALL_PORT to an outdated port whose Portfile sets github.tarball_from tarball"
		return 0
	fi
	dh_setup update "$(port)" --new || return 0
	G17_BRANCH=$(own_branch)
	dh_json check -b "$G17_BRANCH" --on tart || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G17_BRANCH:-}" ] || return 0
	local portfile
	portfile=$(cat "$("$DH_BIN" path "$G17_BRANCH")"/*/"$(port)"/Portfile 2>/dev/null)
	printf '%s\n' "$portfile" | matches -E '^github\.tarball_from[[:space:]]+(archive|releases)' ||
		{ row_fail "$(port)'s Portfile doesn't fetch the archive: $(printf '%s\n' "$portfile" | grep tarball_from | tr '\n' ' ')"; return; }
	matches -F "github.tarball_from tarball is now archive" "$ROW_DIR/out.log" || { row_fail "the update didn't say it moved github.tarball_from to archive"; return; }
	[ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ] || { row_fail "its check in Tart exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null): $(jq -r '.error // empty' "$ROW_DIR/json/1.json" 2>/dev/null)"; return; }
	row_pass "the update of $(port) moved github.tarball_from tarball to archive, said so, and its check in Tart passed"
}
