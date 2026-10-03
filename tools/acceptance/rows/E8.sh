# stages: quick full
# E8: a port with variants, checked with --variants each: the small Rust
# port's update, then check's plan of each variant. --plan only.
port() { printf '%s' "${ACCEPT_RUST_PORT:?}"; }
act() {
	dh update "$(port)" --new || return 0
	dh_json check -p "$(port)" --variants each --plan || :
}
assert() {
	case "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" in
	0) row_pass "planned each variant's build" ;;
	1) row_refused_well "$(jq -r .error "$ROW_DIR/json/1.json")" ;;
	*) row_fail "check --variants each --plan exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" ;;
	esac
}
