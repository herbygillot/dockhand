# stages: quick full
# D-T5: a truncated, and a corrupted, dockhand.db: refused clearly, with
# the file kept as it is, never a panic, and never overwritten. A
# corruption SQLite reads past, in pages nothing uses, may be read.
. "${ROW_LIB:?}/fault.sh"
act() {
	cp "${DOCKHAND_DB:?}" "$ROW_DIR/truncated.db"
	cp "$DOCKHAND_DB" "$ROW_DIR/corrupt.db"
	fault_truncate "$ROW_DIR/truncated.db" 4096
	local n
	for n in 200 300 400 500 600 700 800; do fault_flip "$ROW_DIR/corrupt.db" "$((n * 16))"; done
	shasum -a 256 "$ROW_DIR/truncated.db" "$ROW_DIR/corrupt.db" >"$ROW_DIR/before.sums"
	(export DOCKHAND_DB="$ROW_DIR/truncated.db"; dh_json status) || :
	(export DOCKHAND_DB="$ROW_DIR/corrupt.db"; dh_json status) || :
	shasum -a 256 "$ROW_DIR/truncated.db" "$ROW_DIR/corrupt.db" >"$ROW_DIR/after.sums"
}
assert() {
	if grep -q '^panic:' "$ROW_DIR/out.log"; then
		row_fail "a panic"
	elif [ "$(grep truncated "$ROW_DIR/before.sums")" != "$(grep truncated "$ROW_DIR/after.sums")" ]; then
		row_fail "the truncated database was changed"
	elif [ "$(cat "$ROW_DIR/json/2.json.exit")" != 0 ] && [ "$(grep corrupt "$ROW_DIR/before.sums")" != "$(grep corrupt "$ROW_DIR/after.sums")" ]; then
		row_fail "the corrupt database was refused, and changed"
	elif [ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ]; then
		row_fail "a truncated database read as fine"
	else
		row_pass "refused, files untouched: $(jq -r .error "$ROW_DIR/json/1.json"); corrupt: exit $(cat "$ROW_DIR/json/2.json.exit") $(jq -r '.error // "read"' "$ROW_DIR/json/2.json")"
	fi
}
