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
	# Only what this check's worker writes to drain.log is read: the whole
	# file could credit an earlier check (Codex's cold read at 2cdaf571,
	# #232).
	G8_DRAINED=$(log_size "${DOCKHAND_DB%/*}/drain.log")
	# The journal's progress, this check's alone: the line that says it
	# builds as root is a progress event, which neither drain.log nor
	# dockhand logs of a finished check carries (rc2's G8; the same in
	# v0.8.1).
	G8_SINCE=$(stage_db "SELECT coalesce(max(sequence), 0) FROM events" 2>>"$ROW_DIR/out.log" || echo 0)
	# --fresh: an earlier run's check of the same change on the same master
	# would be reused, and nothing built (#227).
	(export DOCKHAND_CONFIG="$ROW_DIR/g8.toml"; dh_json check -b "$G8_BRANCH" --on local -d --fresh </dev/null) || :
	G8_RUN=$(jq -r '.result.run.name // empty' "$ROW_DIR/json/1.json" 2>/dev/null)
	[ -n "$G8_RUN" ] || return 0
	# The check's owner record, kept while it runs: its directory goes
	# when the check ends, before anyone could read whether the build ran
	# as root and in which session (#332).
	(
		while :; do
			for owner in /var/tmp/dockhand-local-*/owner.json; do
				[ -f "$owner" ] && grep -q "\"check\": *\"$G8_RUN\"" "$owner" 2>/dev/null && cp -p "$owner" "$ROW_DIR/owner.json"
			done
			sleep 1
		done
	) &
	local sampler=$!
	(export DOCKHAND_CONFIG="$ROW_DIR/g8.toml"; dh_json wait "$G8_RUN" </dev/null) || :
	kill "$sampler" 2>/dev/null
	wait "$sampler" 2>/dev/null
	stage_db "SELECT message FROM events WHERE kind = 'progress' AND sequence > ${G8_SINCE:-0}" >"$ROW_DIR/progress.txt" 2>>"$ROW_DIR/out.log" || :
	tail -c +"$((${G8_DRAINED:-0} + 1))" "${DOCKHAND_DB%/*}/drain.log" >"$ROW_DIR/drain.log" 2>/dev/null || :
	# The step that says it builds as root is in the check's own log, not
	# drain.log, which carries only the check's states: rc4's G8 failed a
	# check that built as root and passed (prime-time, 2026-11-17).
	(export DOCKHAND_CONFIG="$ROW_DIR/g8.toml"; dh logs "$G8_RUN" </dev/null) || :
	sed -n "/^\$ dockhand logs $G8_RUN\$/,/^\[exit/p" "$ROW_DIR/out.log" >"$ROW_DIR/logs.txt"
	cat "$ROW_DIR/drain.log" "$ROW_DIR/logs.txt" "$ROW_DIR/progress.txt" >"$ROW_DIR/said.txt" 2>/dev/null || :
}

assert() {
	[ -n "${G8_RAN:-}" ] || return 0
	[ -n "${G8_RUN:-}" ] || { row_fail "check -d queued no check: $(jq -r '.error // empty' "$ROW_DIR/json/1.json" 2>/dev/null)"; return; }
	local wait=$ROW_DIR/json/2.json
	if [ "$(cat "$wait.exit" 2>/dev/null)" != 0 ]; then
		row_fail "$G8_RUN didn't pass under serve's worker, exit $(cat "$wait.exit" 2>/dev/null): $(jq -r '.error // empty' "$wait" 2>/dev/null)"
		return
	fi
	said "building in /opt/local \\(as root\\)" "$ROW_DIR/said.txt" 2>/dev/null || { row_fail "$G8_RUN didn't say it built in /opt/local as root, in its journal, its own log, or what its worker wrote"; return; }
	if [ -s "$ROW_DIR/owner.json" ]; then
		jq -e '.root == true and (.in_session // false) == false' "$ROW_DIR/owner.json" >/dev/null ||
			{ row_fail "$G8_RUN's owner record isn't a root build in a session of its own: $(tr -d '\n' <"$ROW_DIR/owner.json")"; return; }
	else
		echo "# no owner record was sampled while $G8_RUN ran" >>"$ROW_DIR/out.log"
	fi
	local_same /opt/local "$ROW_DIR/active.before" "$G8_RUN left /opt/local's active ports changed" || return
	row_pass "$G8_RUN passed in /opt/local under serve's worker, as root, with sudo asking no password for /opt/local/bin/port-tclsh alone, and put the ports back"
}
