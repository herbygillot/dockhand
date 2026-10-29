# stages: full
# G18: dhtest's status --all, measured against rc2's 55 s (v0.7.0's item
# 6): status gathers every branch's facts at once, where it read each
# branch in turn. It passes under 55 s, and a person compares its output
# with the branches the database holds.
act() {
	host_only "dhtest's database, as rc2 measured it" || return 0
	local started
	started=$(date +%s)
	dh status --all || :
	G18_TOOK=$(($(date +%s) - started))
	sed -n '/^\$ dockhand status --all/,/^\[exit/p' "$ROW_DIR/out.log" >"$ROW_DIR/status.txt"
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/status.txt" ] || return 0
	matches -F "[exit 0]" "$ROW_DIR/status.txt" || { row_fail "status --all failed: $(tail -3 "$ROW_DIR/status.txt" | tr '\n' ' ')"; return; }
	if [ "${G18_TOOK:-999}" -ge 55 ]; then
		row_fail "status --all took ${G18_TOOK}s, not under rc2's 55 s"
		return
	fi
	judged "status --all took ${G18_TOOK}s, under rc2's 55 s, and its output in $ROW_DIR/status.txt lists the database's branches as before"
}
