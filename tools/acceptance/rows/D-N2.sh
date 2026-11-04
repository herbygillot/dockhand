# stages: full
# D-N2: a distfile gone upstream, a port whose old tarball 404s. The
# comparison falls back to MacPorts' mirror, or says it couldn't compare,
# which holds.
#
# It needs such a port, with a newer release, named as ACCEPT_DN2_PORT. A
# port of MacPorts' goes stale as its upstream moves on (termusic 0.9.1
# was one), so host/make-gone-fixture.sh makes one under the test
# account: ACCEPT_DN2_FIXTURE's project, whose v1.0.0 release asset is
# gone and whose v1.1.0 is there, and its port's Portfile, at 1.0.0, in
# the host's fixtures directory, which the row's branch takes as
# devel/<name>/Portfile; full.sh names it as ACCEPT_DN2_PORT where it's
# there. MacPorts' mirror has no copy of a test project's archive, so
# "couldn't compare" is what it reads. Without either, the row isn't run.
dn2_portfile() { printf '%s/fixtures/%s/Portfile' "${ACCEPT_HOST_ROOT:-/Users/Shared/dockhand-acceptance}" "${ACCEPT_DN2_FIXTURE#*/}"; }

act() {
	host_only "a port whose old distfile is gone upstream" || return 0
	if [ -z "${ACCEPT_DN2_PORT:-}" ]; then
		row_result "not run" "no port whose current distfile 404s upstream, with a newer release, was named as ACCEPT_DN2_PORT: run tools/acceptance/host/make-gone-fixture.sh --run as the test account, then set ACCEPT_DN2_FIXTURE to the project it made"
		return 0
	fi
	if [ -n "${ACCEPT_DN2_FIXTURE:-}" ] && [ "$ACCEPT_DN2_PORT" = "${ACCEPT_DN2_FIXTURE#*/}" ]; then
		local branch worktree dir
		branch=$(run_name dn2)
		dh_setup start "$branch" || return 0
		worktree=$("$DH_BIN" path "$branch") || { row_fail "dockhand path $branch found no worktree"; return 0; }
		dir="$worktree/devel/$ACCEPT_DN2_PORT"
		mkdir -p "$dir" && cp "$(dn2_portfile)" "$dir/Portfile" || { row_fail "couldn't put $(dn2_portfile) in $dir"; return 0; }
		git -C "$worktree" add "devel/$ACCEPT_DN2_PORT/Portfile" &&
			git -C "$worktree" -c user.name=acceptance -c user.email=acceptance@example.invalid -c commit.gpgsign=false \
				commit -q -m "$ACCEPT_DN2_PORT: new port, 1.0.0" || { row_fail "couldn't commit the fixture's port on $branch"; return 0; }
		dh_json update "$ACCEPT_DN2_PORT" -b "$branch" --plan || :
		return 0
	fi
	dh_json update "$ACCEPT_DN2_PORT" --new --plan || :
}
assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	jq -e '.. | strings | select(test("mirror|couldn.t compare|not compared"))' "$ROW_DIR/json/1.json" >/dev/null &&
		row_pass "fell back to the mirror, or said it couldn't compare" ||
		row_fail "neither the mirror nor a hold: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"
}
