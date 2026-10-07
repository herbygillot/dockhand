# stages: selftest
# H6 holds: a Next: line naming a branch the row then closed the pull
# request of isn't run (next_superseded_for).
setup() { touch "$FAKE_DH_STATE/refuse.tidy"; }
act() {
	dh hint
	next_superseded_for x "its test pull request closed"
}
assert() { row_pass; }
