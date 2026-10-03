# stages: quick full
# C6: subports. A GitHub Go port with subports, terraform-1.16, works or
# refuses well; a py- port, py-lmdb, outside the supported scope, refuses
# well or plans an edit a person judges. --plan only; the plans are kept
# for that judgment.
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
	elif [ "$(cat "$C6_PY.exit")" = 0 ]; then
		row_known "py-lmdb planned an edit, outside the supported scope; judge it in $ROW_DIR/out.log"
	else
		row_pass "terraform-1.16 $(sed 's/^0$/planned/; s/^1$/refused/' "$C6_TERRAFORM.exit"); py-lmdb refused: $(jq -r .error "$C6_PY")"
	fi
}
