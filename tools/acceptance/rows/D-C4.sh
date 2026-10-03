# stages: full
# D-C4: GH_TOKEN set to a token for another account: the account it acts
# as is named before any push (H2).
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a second account's token" || return 0
	checkpoint "put a token for another GitHub account in $ROW_DIR/token (deleted after the row)" || return 0
	GH_TOKEN=$(cat "$ROW_DIR/token") "$DH_BIN" auth status >>"$ROW_DIR/out.log" 2>&1 || :
	GH_TOKEN=$(cat "$ROW_DIR/token") "$DH_BIN" submit -p "$(port)" --plan >>"$ROW_DIR/out.log" 2>&1 || :
	rm -f "$ROW_DIR/token"
}
assert() {
	judged "auth status and submit's preview named the other account, and nothing was pushed"
}
