# stages: full
# D-C6: Git's own push credentials missing: an HTTPS fork remote with no
# credential helper, and an SSH key with a passphrase and no agent, at a
# terminal and under serve. It fails naming git's credentials, never
# hangs.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "the fork's push credentials" || return 0
	checkpoint "point the fork remote at HTTPS with no credential helper (git config --global --unset credential.helper)" || return 0
	with_timeout 300 "$DH_BIN" submit -p "$(port)" -y >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	checkpoint "put the fork remote back on SSH with a passphrase key and no agent (ssh-add -D)" || return 0
	with_timeout 300 "$DH_BIN" submit -p "$(port)" -y >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
}
assert() {
	grep -q '^\[exit 142\]' "$ROW_DIR/out.log" && { row_fail "a push hung"; return; }
	grep -qiE 'credential|passphrase|ssh' "$ROW_DIR/out.log" &&
		row_pass "each failed naming git's credentials" || row_fail "the failure didn't name git's credentials"
}
