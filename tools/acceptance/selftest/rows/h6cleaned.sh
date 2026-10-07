# stages: selftest
# H6 holds: a Next: line naming a branch the row then cleaned isn't run,
# as B6's after its merge (clean_before and clean_after).
setup() {
	touch "$FAKE_DH_STATE/refuse.tidy"
	echo '{"attention": [], "branches": [{"name": "x"}]}' >"$FAKE_DH_STATE/status.json"
}
act() {
	dh hint
	dh clean -y
}
assert() { row_pass; }
