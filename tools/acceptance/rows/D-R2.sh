# stages: quick full
# D-R2: both VM slots taken by the person's own Tart VMs. The check waits
# or says why, and never stops those VMs. It needs a vanilla image to
# clone, ACCEPT_VANILLA, else Cirrus Labs' vanilla image of macOS 27
# where Tart has it already; without one, it isn't run.
. "${ROW_LIB:?}/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
setup() {
	if [ -z "${ACCEPT_VANILLA:-}" ] && tart list --quiet 2>/dev/null | grep -qxF ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest; then
		export ACCEPT_VANILLA=ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest
	fi
	[ -n "${ACCEPT_VANILLA:-}" ] || return 0
	fault_vm_slots
}
act() {
	[ -n "${ACCEPT_VANILLA:-}" ] || return 0
	allow_running "vm dhaccept-slot-1" "vm dhaccept-slot-2"
	dh_setup update "$(port)" --new || return 0
	# --fresh, so a reused build doesn't skip the guest the fault is for.
	with_timeout 180 "$DH_BIN" check -b "$(own_branch)" --fresh >"$ROW_DIR/dr2.log" 2>&1
	echo "$?" >"$ROW_DIR/dr2.exit"
	cat "$ROW_DIR/dr2.log" >>"$ROW_DIR/out.log"
	DR2_RUNNING=$(tart list --format json | jq -r '[.[] | select(.Name | startswith("dhaccept-slot-")) | select(.State == "running")] | length')
}
assert() {
	if [ -z "${ACCEPT_VANILLA:-}" ]; then
		row_result "not run" "ACCEPT_VANILLA names no vanilla image to take the slots with, and Tart hasn't Cirrus Labs' macOS 27 one"
		return
	fi
	if [ "${DR2_RUNNING:-0}" != 2 ]; then
		row_fail "the person's VMs didn't survive the check: $DR2_RUNNING of 2 running"
	elif grep -qiE 'slot|wait|capacity|running VM' "$ROW_DIR/dr2.log"; then
		row_pass "the check waited or said why: $(grep -iE 'slot|wait|capacity|running VM' "$ROW_DIR/dr2.log" | head -1)"
	else
		row_fail "the check neither waited nor said why: $(tail -3 "$ROW_DIR/dr2.log" | tr '\n' ';')"
	fi
}

# The slot VMs are stopped whatever happened, a setup that failed with
# them started included.
teardown() { fault_vm_slots_stop; }
