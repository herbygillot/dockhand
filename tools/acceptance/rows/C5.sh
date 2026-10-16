# stages: quick full
# C5: a batch's notes. Each port ship carries in a batch says what its
# single update says: what comparing upstream found, a comparison that
# couldn't be made, a new major version, a pin dropped; or, where nobody
# looks, what held it. Each port's single update is planned from master to
# compare. No check is run.
ports() { printf '%s %s doge' "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}"; }

act() {
	local port
	for port in $(ports); do
		dh update "$port" --new --plan </dev/null || :
		sed -n "/^\\\$ dockhand update $port --new --plan/,/^\\[exit/p" "$ROW_DIR/out.log" >"$ROW_DIR/single.$port"
	done
	# shellcheck disable=SC2046
	dh ship $(ports) --to commit -y </dev/null || :
	sed -n '/^\$ dockhand ship/,/^\[exit/p' "$ROW_DIR/out.log" >"$ROW_DIR/batch.log"
}

assert() {
	local port missing="" note
	for port in $(ports); do
		for note in 'A new major version' 'so the pin is dropped' 'Upstream archives not compared'; do
			if grep -q "$note" "$ROW_DIR/single.$port" && ! grep -q "$note" "$ROW_DIR/batch.log"; then
				missing="$missing $port($note)"
			fi
		done
	done
	if [ -n "$missing" ]; then
		row_fail "the batch left out notes a single update says:$missing"
	elif ! grep -qE '^  [^ ]+ +a version update from master, to commit$' "$ROW_DIR/batch.log"; then
		row_fail "the batch printed no plan rows: $(tail -3 "$ROW_DIR/batch.log" | tr '\n' ';')"
	else
		row_pass "each port's update says the notes its single update says"
	fi
}
