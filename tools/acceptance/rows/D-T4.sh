# stages: quick full
# D-T4: interactive commands without a terminal: tidy on a hand edit,
# clean, submit --passing, and authoring on master where two branches
# change the port. Each refuses, naming the -y or -b it needs, and none
# hangs.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh_setup start $(run_name dt4-a) --port "$(port)" || return 0
	dh_setup start $(run_name dt4-b) --port "$(port)" || return 0
	local dir
	for b in $(run_name dt4-a) $(run_name dt4-b); do
		dir=$("$DH_BIN" path "$b")
		printf '# a hand edit\n' >>"$(find "$dir" -path "*/$(port)/Portfile" | head -1)"
	done
	allow_change "*/Portfile"
	for args in "tidy -b $(run_name dt4-a)" "clean" "submit --passing" "update $(port)"; do
		# shellcheck disable=SC2086
		with_timeout 120 "$DH_BIN" $args </dev/null >"$ROW_DIR/dt4.$(echo $args | tr ' /' '__').log" 2>&1
		echo "$args: $?" >>"$ROW_DIR/dt4.exits"
		cat "$ROW_DIR/dt4.$(echo $args | tr ' /' '__').log" >>"$ROW_DIR/out.log"
	done
}
assert() {
	if grep -q ': 142$' "$ROW_DIR/dt4.exits"; then
		row_fail "hung without a terminal: $(grep ': 142$' "$ROW_DIR/dt4.exits" | tr '\n' ';')"
		return
	fi
	local tidy update
	tidy=$(cat "$ROW_DIR/dt4.tidy_-b_$(run_name dt4-a).log")
	update=$(cat "$ROW_DIR/dt4.update_${ACCEPT_GO_PORT}.log")
	if ! printf '%s' "$tidy" | grep -qE -- '-y|--yes|terminal|--message|review'; then
		row_fail "tidy without a terminal didn't say what it needs: $tidy"
	elif ! printf '%s' "$update" | grep -q -- '-b'; then
		row_fail "update with two branches changing the port didn't name -b: $update"
	else
		row_pass "none hung; each said what it needs: $(tr '\n' ';' <"$ROW_DIR/dt4.exits")"
	fi
}
