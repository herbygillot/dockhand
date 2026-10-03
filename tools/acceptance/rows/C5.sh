# stages: quick full
# C5: update --outdated's notes. Each port of a batch carries, under its
# line, the notes a single update of it says: what comparing upstream
# found, a comparison that couldn't be made, a new major version, a pin
# dropped. Each port's single update is planned from master to compare.
# No check is run.
ports() { printf '%s %s doge' "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}"; }

# c5_notes is the notes under a port's line, as one line each, trimmed.
c5_notes() {
	awk -v port="$1" '
		$0 ~ "^  [✓✗] " port "-" {on = 1; next}
		on && /^      / {sub(/^ +/, ""); print; next}
		on {exit}
	' "$2"
}

act() {
	local port
	for port in $(ports); do
		dh update "$port" --new --plan </dev/null || :
		sed -n "/^\\\$ dockhand update $port --new --plan/,/^\\[exit/p" "$ROW_DIR/out.log" >"$ROW_DIR/single.$port"
	done
	dh update --outdated $(ports) -y </dev/null || :
	sed -n '/^\$ dockhand update --outdated/,/^\[exit/p' "$ROW_DIR/out.log" >"$ROW_DIR/batch.log"
}

assert() {
	local port missing="" note
	for port in $(ports); do
		for note in 'A new major version' 'so the pin is dropped' 'Upstream archives not compared'; do
			if grep -q "$note" "$ROW_DIR/single.$port" && ! c5_notes "$port" "$ROW_DIR/batch.log" | grep -q "$note"; then
				missing="$missing $port($note)"
			fi
		done
	done
	if [ -n "$missing" ]; then
		row_fail "the batch left out notes a single update says:$missing"
	elif ! grep -qE '^  [✓✗] ' "$ROW_DIR/batch.log"; then
		row_fail "the batch printed no port lines: $(tail -3 "$ROW_DIR/batch.log" | tr '\n' ';')"
	else
		row_pass "each port's line carries the notes its single update says"
	fi
}
