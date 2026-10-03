# stages: quick full
# C3: cargo.update yes (D19), on doge and idevice_pair, which declare it:
# one whose archive ships a Cargo.lock works, with a notice; one whose
# doesn't is refused; neither plan adds or removes the cargo.update line.
. "${ROW_LIB:?}/plan.sh"
act() {
	C3_DOGE=$(plan_update doge)
	C3_PAIR=$(plan_update idevice_pair)
}
assert() {
	local file port
	for port in doge idevice_pair; do
		file=$C3_DOGE
		[ "$port" = idevice_pair ] && file=$C3_PAIR
		if ! plan_ok "$file"; then
			row_fail "$port's plan exited $(cat "$file.exit"): $(jq -r '.error // empty' "$file")"
			return
		fi
		if plan_words "$port" | grep -qE '^[-+][[:space:]]*cargo\.update'; then
			row_fail "$port's plan adds or removes the cargo.update line"
			return
		fi
	done
	row_pass "doge: $(cat "$C3_DOGE.exit" | sed 's/^0$/planned/; s/^1$/refused/'); idevice_pair: $(cat "$C3_PAIR.exit" | sed 's/^0$/planned/; s/^1$/refused/'); the cargo.update line left alone"
}
