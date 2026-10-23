# stages: quick full
# C6: subports. A GitHub Go port with subports, terraform-1.16, works or
# refuses well; a py- port, py-lmdb, whose subports share its Portfile,
# plans its edit. The M1 judged that plan sound on rc3, rc4, and rc5, so a
# refusal fails, where it was v0.4.0's known issue. --plan only; the plans
# are kept in out.log.
. "${ROW_LIB:?}/plan.sh"
act() {
	C6_TERRAFORM=$(plan_update terraform-1.16)
	C6_PY=$(plan_update py-lmdb)
}
assert() {
	if ! plan_ok "$C6_TERRAFORM"; then
		row_fail "terraform-1.16's plan exited $(cat "$C6_TERRAFORM.exit"): $(jq -r '.error // empty' "$C6_TERRAFORM")"
	elif ! plan_ok "$C6_PY"; then
		row_fail "py-lmdb's plan exited $(cat "$C6_PY.exit"): $(jq -r '.error // empty' "$C6_PY")"
	elif [ "$(cat "$C6_PY.exit")" != 0 ]; then
		row_fail "py-lmdb was refused, where rc3 to rc5 planned it: $(jq -r '.error // empty' "$C6_PY")"
	else
		row_pass "terraform-1.16 $(sed 's/^0$/planned/; s/^1$/refused/' "$C6_TERRAFORM.exit"); py-lmdb planned, its plan in $ROW_DIR/out.log"
	fi
}
