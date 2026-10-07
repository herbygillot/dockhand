# stages: quick full
# C8: tidy -y and saved plans. -y applies a plan of dockhand's own edits;
# on a branch with a hand edit, without a terminal, it refuses; and a plan
# saved with --plan --out is refused when its branch's files have moved
# since, naming why.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }

act() {
	dh_setup update "$(port)" --new || return 0
	dh_json tidy -p "$(port)" -y || :
	dh_setup start $(run_name c8-hand) --port "${ACCEPT_GO_PORT:?}" || return 0
	local dir portfile
	dir=$("$DH_BIN" path $(run_name c8-hand))
	portfile=$(find "$dir" -path "*/${ACCEPT_GO_PORT}/Portfile" | head -1)
	printf '# a hand edit\n' >>"$portfile"
	allow_change "$portfile"
	dh_json tidy -b $(run_name c8-hand) -y </dev/null || :
	dh tidy -b $(run_name c8-hand) --plan --out "$ROW_DIR/c8.plan" </dev/null || :
	printf '# and another, after the plan was saved\n' >>"$portfile"
	(cd "$dir" && dh_json tidy --apply "$ROW_DIR/c8.plan" </dev/null) || :
}

assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ] || { row_fail "tidy -y on dockhand's own edits exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)"; return; }
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" != 0 ] || { row_fail "tidy -y applied a plan with a hand edit, without a terminal"; return; }
	[ -s "$ROW_DIR/c8.plan" ] || { row_fail "tidy --plan --out saved no plan"; return; }
	if [ "$(cat "$ROW_DIR/json/3.json.exit" 2>/dev/null)" = 0 ]; then
		row_fail "a saved plan applied after the branch's files moved"
		return
	fi
	if ! jq -r '.error // ""' "$ROW_DIR/json/3.json" | grep -qiE 'as they were|changed|since|saved'; then
		row_fail "the stale plan's refusal didn't say why: $(jq -r .error "$ROW_DIR/json/3.json")"
		return
	fi
	row_pass "applied, refused the hand edit, and refused the stale plan: $(jq -r .error "$ROW_DIR/json/3.json")"
}
