# stages: selftest
# Breaks H6: a Next: line names a step dockhand then refuses.
setup() { touch "$FAKE_DH_STATE/refuse.tidy"; }
act() { dh hint; }
assert() { row_pass; }
