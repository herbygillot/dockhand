# stages: full
# G18: status --all, measured against rc2's 55 s (v0.7.0's item 6): status
# gathers every branch's facts at once, where it read each branch in turn.
# It passes under 55 s, and a person compares its output with the branches
# the database holds.
#
# It times a fixed copy of dhtest's database, ACCEPT_G18_DB,
# ~/.dockhand-acceptance/g18-fixed.db unless set, never the live one: the
# live one grows every candidate, and rc4 timed 57 s on 1,400 branches,
# the same status with more data (#224). The row makes the copy with
# dockhand backup the first time and keeps it, so each run measures
# dockhand on the same database. The candidate opens a copy of the copy,
# with automatic cleanup off, so the fixed one is never migrated or
# written.
g18_fixed() { printf '%s' "${ACCEPT_G18_DB:-$HOME/.dockhand-acceptance/g18-fixed.db}"; }
act() {
	host_only "dhtest's database, as rc2 measured it" || return 0
	local fixed made copy config started
	fixed=$(g18_fixed)
	if [ ! -f "$fixed" ]; then
		mkdir -p "$ROW_DIR/backup" "$(dirname "$fixed")"
		dh_json backup "$ROW_DIR/backup" || :
		made=$(ls "$ROW_DIR"/backup/dockhand-*.db 2>/dev/null | head -1)
		[ -n "$made" ] || { row_fail "dockhand backup made no copy to fix G18's database with: $(tail -3 "$ROW_DIR/out.log" | tr '\n' ';')"; return 0; }
		mv "$made" "$fixed"
		printf '# G18 fixed its database at %s, %s branches, from this run\n' "$fixed" "$(sqlite3 "$fixed" 'SELECT count(*) FROM branches' 2>/dev/null)" >>"$ROW_DIR/out.log"
	fi
	copy="$ROW_DIR/g18.db"
	cp "$fixed" "$copy"
	config="$ROW_DIR/g18.toml"
	if [ -f "${DOCKHAND_CONFIG:-}" ] && ! grep -q '^\[cleanup\]' "$DOCKHAND_CONFIG"; then
		cp "$DOCKHAND_CONFIG" "$config"
	else
		: >"$config"
	fi
	printf '\n[cleanup]\nautomatic = false\n' >>"$config"
	started=$(date +%s)
	DOCKHAND_DB=$copy DOCKHAND_CONFIG=$config dh status --all || :
	G18_TOOK=$(($(date +%s) - started))
	G18_BRANCHES=$(sqlite3 "$fixed" 'SELECT count(*) FROM branches' 2>/dev/null || echo "?")
	sed -n '/^\$ dockhand status --all/,/^\[exit/p' "$ROW_DIR/out.log" >"$ROW_DIR/status.txt"
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/status.txt" ] || return 0
	matches -F "[exit 0]" "$ROW_DIR/status.txt" || { row_fail "status --all failed: $(tail -3 "$ROW_DIR/status.txt" | tr '\n' ' ')"; return; }
	if [ "${G18_TOOK:-999}" -ge 55 ]; then
		row_fail "status --all took ${G18_TOOK}s on the fixed copy of ${G18_BRANCHES} branches, not under rc2's 55 s"
		return
	fi
	judged "status --all took ${G18_TOOK}s on the fixed copy of ${G18_BRANCHES} branches, under rc2's 55 s, and its output in $ROW_DIR/status.txt lists that database's branches as before"
}
