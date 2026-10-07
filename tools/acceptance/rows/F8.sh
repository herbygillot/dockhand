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
	# Homebrew's Tart, ahead on PATH, stood in for by a Tart of its own in
	# a Homebrew-shaped directory, saying a newer version and doing what
	# the real one does, so no one installs Homebrew for the row.
	local brew=$ROW_DIR/homebrew/bin real
	real=$(command -v tart)
	mkdir -p "$brew"
	printf '#!/bin/sh
[ "$1" = --version ] && { echo "2.41.0"; exit 0; }
exec "%s" "$@"
' "$real" >"$brew/tart"
	chmod +x "$brew/tart"
	PATH="$brew:$PATH" dh_json providers || :
}
assert() {
	grep -q '2.39' "$ROW_DIR/out.log" || { row_fail "the old Tart wasn't refused by version"; return; }
	grep -q "Tart isn't installed" "$ROW_DIR/out.log" && { row_fail "the Tart ahead on PATH wasn't found"; return; }
	row_pass "the old Tart was refused by version, and the Tart ahead on PATH was found"
}
