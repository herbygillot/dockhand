# stages: quick full
# E1: subports sharing a Portfile, terraform-1.16 and py-lmdb: a correct
# plan or a clean refusal, never a wrong edit. --plan only; the plans are
# kept for a person's judgment.
. "${ROW_LIB:?}/plan.sh"
act() { E1_A=$(plan_update terraform-1.16); E1_B=$(plan_update py-lmdb); }
assert() {
	plan_ok "$E1_A" && plan_ok "$E1_B" || { row_fail "a plan crashed: $(cat "$E1_A.exit") $(cat "$E1_B.exit")"; return; }
	row_pass "terraform-1.16: $(sed 's/^0$/planned/; s/^1$/refused/' "$E1_A.exit"); py-lmdb: $(sed 's/^0$/planned/; s/^1$/refused/' "$E1_B.exit") $(jq -r '.error // ""' "$E1_B")"
}
