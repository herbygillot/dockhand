# stages: full
# G8: serve checks in /opt/local under the one sudoers line for its
# port-tclsh (v0.7.0): with dhtest's sudo asking no password for
# /opt/local/bin/port-tclsh and for nothing else, a check queued for
# /opt/local runs in serve's background worker, which has no terminal to
# ask on, builds as root, and puts /opt/local's ports back as they were.
. "${ROW_LIB:?}/local.sh"

act() {
	host_only "dhtest's sudoers line for /opt/local's port-tclsh" || return 0
	checkpoint "with visudo, give dhtest the one rule that asks no password, dhtest ALL=(root) NOPASSWD: /opt/local/bin/port-tclsh, with no other NOPASSWD rule of dhtest's" || return 0
	# The rule is the one: port-tclsh asks nothing, and nothing else is let.
	if ! sudo -n /opt/local/bin/port-tclsh /dev/null 2>/dev/null; then
		row_result "not run" "sudo asked a password of /opt/local/bin/port-tclsh: the sudoers line isn't in place"
		return 0
	fi
	if sudo -n /usr/bin/true 2>/dev/null; then
		row_result "not run" "another sudo rule lets dhtest run /usr/bin/true with no password, so this wouldn't show the one line is enough"
		return 0
	fi
	G8_RAN=1
	local_active /opt/local >"$ROW_DIR/active.before"
	cat "${DOCKHAND_CONFIG:?}" >"$ROW_DIR/g8.toml"
	printf '\n[providers.local]\nserve = true\n' >>"$ROW_DIR/g8.toml"
	G8_BRANCH=$(run_name g8-sudo)
	dh_setup revbump "${ACCEPT_GO_PORT:?}" --new="$G8_BRANCH" --subject "rebuild to check under serve's sudoers line" || return 0
	# -d with no serve running starts a background worker, serve's, with
	# no terminal: where sudo would ask, it refuses.
	(export DOCKHAND_CONFIG="$ROW_DIR/g8.toml"; dh_json check -b "$G8_BRANCH" --on local -d </dev/null) || :
	G8_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json" 2>/dev/null)
	[ -n "$G8_RUN" ] || return 0
	(export DOCKHAND_CONFIG="$ROW_DIR/g8.toml"; dh_json wait "$G8_RUN" </dev/null) || :
}

assert() {
	[ -n "${G8_RAN:-}" ] || return 0
	[ -n "${G8_RUN:-}" ] || { row_fail "check -d queued no check: $(jq -r '.error // empty' "$ROW_DIR/json/1.json" 2>/dev/null)"; return; }
	local wait=$ROW_DIR/json/2.json
	if [ "$(cat "$wait.exit" 2>/dev/null)" != 0 ]; then
		row_fail "$G8_RUN didn't pass under serve's worker, exit $(cat "$wait.exit" 2>/dev/null): $(jq -r '.error // empty' "$wait" 2>/dev/null)"
		return
	fi
	grep -q "building in /opt/local (as root)" "$ROW_DIR/out.log" "${DOCKHAND_DB%/*}/drain.log" 2>/dev/null || { row_fail "$G8_RUN didn't say it built in /opt/local as root"; return; }
	local_same /opt/local "$ROW_DIR/active.before" "$G8_RUN left /opt/local's active ports changed" || return
	row_pass "$G8_RUN passed in /opt/local under serve's worker, as root, with sudo asking no password for /opt/local/bin/port-tclsh alone, and put the ports back"
}
