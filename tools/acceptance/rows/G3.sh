# stages: full
# G3: a prefix installed without root, listed in [providers.local] clean:
# the check needs no sudo, builds each target with all but its
# dependencies deactivated, and puts the prefix's ports back after.
# ACCEPT_LOCAL_PREFIX names the prefix, which MacPorts' source install
# made as dhtest (./configure --prefix=<it> --with-no-root-privileges,
# make, make install, port selfupdate); without one, the row isn't run.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "a MacPorts prefix installed without root" || return 0
	G3_PREFIX=${ACCEPT_LOCAL_PREFIX:-}
	if [ -z "$G3_PREFIX" ] || [ ! -x "$G3_PREFIX/bin/port" ]; then
		row_result "not run" "set ACCEPT_LOCAL_PREFIX to a MacPorts prefix dhtest installed without root"
		G3_PREFIX=""
		return 0
	fi
	local_active "$G3_PREFIX" >"$ROW_DIR/active.before"
	cat "${DOCKHAND_CONFIG:?}" >"$ROW_DIR/g3.toml"
	printf '\n[providers.local]\nclean = ["%s"]\n' "$G3_PREFIX" >>"$ROW_DIR/g3.toml"
	G3_BRANCH=$(run_name g3-clean)
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G3_BRANCH" --subject "rebuild to check in a prefix of dhtest's" || return 0
	# No sudo here: one asked for would fail the check, not prompt.
	(export DOCKHAND_CONFIG="$ROW_DIR/g3.toml"; dh_json check -b "$G3_BRANCH" --on "local:$G3_PREFIX" </dev/null) || :
}

assert() {
	[ -n "${G3_PREFIX:-}" ] && [ -n "${G3_BRANCH:-}" ] || return 0
	local file=$ROW_DIR/json/1.json
	if [ "$(cat "$file.exit")" != 0 ]; then
		row_fail "the check in $G3_PREFIX exited $(cat "$file.exit"): $(jq -r '.error // ""' "$file")"
		return
	fi
	grep -q "building in $G3_PREFIX (as you)" "$ROW_DIR/out.log" || { row_fail "the check in $G3_PREFIX didn't build as dhtest"; return; }
	grep -q "MacPorts' binary archives are built for /opt/local alone" "$ROW_DIR/out.log" || { row_fail "the plan didn't say $G3_PREFIX's dependencies build from source"; return; }
	local_same "$G3_PREFIX" "$ROW_DIR/active.before" "the check left $G3_PREFIX's active ports changed" || return
	row_pass "built in $G3_PREFIX with no sudo, clean, and put its ports back"
}
