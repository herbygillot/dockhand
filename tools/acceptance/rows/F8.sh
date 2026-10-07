# stages: full
# F8: Tart from Homebrew rather than MacPorts, and Tart older than 2.39
# with a Golden Gate image: found; the old Tart refused before anything
# clones.
. "$ROW_LIB/fault.sh"
# Its check names a branch of its own, made in setup: with earlier rows'
# branches set aside, check -p found none (the rc6 full stage).
setup() {
	host_only "Tart on the host" || return 0
	dh_setup update "${ACCEPT_GO_PORT:?}" --new || return 1
	F8_BRANCH=$(own_branch)
}
act() {
	host_only "Tart on the host" || return 0
	local shim
	shim=$(fault_shims old-tart)
	PATH="$shim:$PATH" "$DH_BIN" check -b "$F8_BRANCH" --on golden-gate >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	checkpoint "install Tart from Homebrew too (brew install cirruslabs/cli/tart), ahead on PATH" || return 0
	dh_json providers || :
}
assert() {
	grep -q '2.39' "$ROW_DIR/out.log" || { row_fail "the old Tart wasn't refused by version"; return; }
	judged "Homebrew's Tart was found"
}
