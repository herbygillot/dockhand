# stages: quick full
# D-S8: a dirty main checkout, and running from a subdirectory or outside
# any checkout. Commands that need a clean checkout refuse by name; --tree
# and $MACPORTS_TREE are honoured.
act() {
	printf 'dirty\n' >"$MACPORTS_TREE/_resources/DIRTY.txt"
	allow_change "$MACPORTS_TREE/_resources/DIRTY.txt"
	git -C "$MACPORTS_TREE" add _resources/DIRTY.txt
	(cd "$MACPORTS_TREE/_resources" && dh_json status) || :
	(cd / && dh_json --tree "$MACPORTS_TREE" status) || :
	(cd / && dh_json status) || :
	(cd / && env -u MACPORTS_TREE "$DH_BIN" --json status >"$ROW_DIR/outside.json" 2>&1) || :
	dh_json start ds8 --here </dev/null || :
}
# The staged file goes again, from the index and the checkout: it stayed
# staged in the full stage's checkout from 2026-10-09 until a driver
# removed it by hand (prime-time, 2026-10-16).
teardown() {
	git -C "$MACPORTS_TREE" rm -q --cached --ignore-unmatch _resources/DIRTY.txt 2>/dev/null || :
	rm -f "$MACPORTS_TREE/_resources/DIRTY.txt"
	return 0
}
assert() {
	[ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ] || { row_fail "status from a subdirectory exited $(cat "$ROW_DIR/json/1.json.exit")"; return; }
	[ "$(cat "$ROW_DIR/json/2.json.exit")" = 0 ] || { row_fail "--tree wasn't honoured from /"; return; }
	[ "$(cat "$ROW_DIR/json/3.json.exit")" = 0 ] || { row_fail "\$MACPORTS_TREE wasn't honoured from /"; return; }
	jq -e '.exit_code != 0 and (.error // "" | length > 0)' "$ROW_DIR/outside.json" >/dev/null || { row_fail "outside any checkout, status didn't refuse by name: $(head -c 300 "$ROW_DIR/outside.json")"; return; }
	if [ "$(cat "$ROW_DIR/json/4.json.exit")" = 0 ]; then
		row_fail "start --here went on in a dirty checkout"
		return
	fi
	row_pass "subdirectory, --tree, and \$MACPORTS_TREE honoured; outside a checkout refused; start --here refused the dirty checkout: $(jq -r .error "$ROW_DIR/json/4.json")"
}
