# stages: selftest
# Breaks H4: a token the row writes into a secret directory.
setup() {
	export ACCEPT_SECRET_DIRS="$ROW_DIR/secrets"
	mkdir -p "$ACCEPT_SECRET_DIRS"
}
act() { echo "token gho_abcdefghijklmnopqrstuvwxyz0123456789" >"$ACCEPT_SECRET_DIRS/leaked"; }
assert() { row_pass; }
