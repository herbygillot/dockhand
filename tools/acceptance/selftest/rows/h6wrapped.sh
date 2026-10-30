# stages: selftest
# Breaks H6: a Next: line dockhand wrapped is joined again, so the step on
# its later line is run, and dockhand refuses it.
setup() { touch "$FAKE_DH_STATE/refuse.tidy"; }
act() { dh wrapped; }
assert() { row_pass; }
