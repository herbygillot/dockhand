# stages: selftest
# A token already in a secret directory before the row is no harm of its:
# H4 compares what the row found with what was there.
setup() {
	export ACCEPT_SECRET_DIRS="$ROW_DIR/secrets"
	mkdir -p "$ACCEPT_SECRET_DIRS"
	echo "token gho_0123456789abcdefghijklmnopqrstuvwxyz" >"$ACCEPT_SECRET_DIRS/before"
}
assert() { row_pass; }
