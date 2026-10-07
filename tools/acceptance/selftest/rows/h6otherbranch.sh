# stages: selftest
# Breaks H6: a mark for another branch leaves this one's Next: lines to
# run, and dockhand refuses the step one names.
setup() { touch "$FAKE_DH_STATE/refuse.tidy"; }
act() {
	dh hint
	next_superseded_for y "another branch's test pull request closed"
}
assert() { row_pass; }
