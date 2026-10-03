# stages: quick full
# C10: SI sizes. cleanup.min_free reads 30GB, 1.5TB, and 500MB in SI
# units and GiB as binary, and config shows each with what it reads as; a
# size that isn't one is refused by name.

c10_config() {
	local value=$1
	printf '[cleanup]\nmin_free = "%s"\n' "$value" >"$ROW_DIR/c10.toml"
	(export DOCKHAND_CONFIG="$ROW_DIR/c10.toml"; dh_json config) || :
}

act() {
	for value in 30GB 1.5TB 500MB 30GiB "30 parsecs"; do
		c10_config "$value"
	done
}

assert() {
	local n=1 want got
	for want in "30GB (30 GB)" "1.5TB (1.5 TB)" "500MB (500 MB)" "30GiB (32 GB)"; do
		got=$(jq -r '.result.settings[]? | select(.key == "cleanup.min_free") | .value' "$ROW_DIR/json/$n.json")
		if [ "$got" != "$want" ]; then
			row_fail "cleanup.min_free reads as [$got], not [$want]"
			return
		fi
		n=$((n + 1))
	done
	if [ "$(cat "$ROW_DIR/json/5.json.exit")" = 0 ] || ! jq -r .error "$ROW_DIR/json/5.json" | grep -q 'cleanup.min_free: "30 parsecs" is not a size'; then
		row_fail "a size that isn't one wasn't refused by name: $(jq -r .error "$ROW_DIR/json/5.json")"
		return
	fi
	row_pass "SI and binary sizes read and shown; a bad one refused by name"
}
