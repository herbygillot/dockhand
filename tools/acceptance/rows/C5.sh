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
	local port missing="" single
	# Each note by its own pattern, which the gate holds to what update
	# prints (#223): update had come to word all three otherwise, and a
	# loop over the old words found none in either log, passing on nothing.
	for port in $(ports); do
		single=$ROW_DIR/single.$port
		if said 'New major version: ports that depend on' "$single" && ! said 'New major version: ports that depend on' "$ROW_DIR/batch.log"; then
			missing="$missing $port(a new major version)"
		fi
		if said 'Removed the Portfile pin for ' "$single" && ! said 'Removed the Portfile pin for ' "$ROW_DIR/batch.log"; then
			missing="$missing $port(a pin dropped)"
		fi
		if said 'Upstream not compared: ' "$single" && ! said 'Upstream not compared: ' "$ROW_DIR/batch.log"; then
			missing="$missing $port(upstream not compared)"
		fi
	done
	if [ -n "$missing" ]; then
		row_fail "the batch left out notes a single update says:$missing"
	elif ! said '^  [^ ]+ +a version update from master, to commit$' "$ROW_DIR/batch.log"; then
		row_fail "the batch printed no plan rows: $(tail -3 "$ROW_DIR/batch.log" | tr '\n' ';')"
	else
		row_pass "each port's update says the notes its single update says"
	fi
}
