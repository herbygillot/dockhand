# stages: full
# F8: Tart from Homebrew rather than MacPorts, and Tart older than 2.39
# with a Golden Gate image: found; the old Tart refused before anything
# clones.
. "$ROW_LIB/fault.sh"
act() {
	host_only "Tart on the host" || return 0
	local shim
	shim=$(fault_shims old-tart)
	PATH="$shim:$PATH" "$DH_BIN" check -p "${ACCEPT_GO_PORT:?}" --on golden-gate >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	checkpoint "install Tart from Homebrew too (brew install cirruslabs/cli/tart), ahead on PATH" || return 0
	dh_json providers || :
}
assert() {
	grep -q '2.39' "$ROW_DIR/out.log" || { row_fail "the old Tart wasn't refused by version"; return; }
	judged "Homebrew's Tart was found"
}
