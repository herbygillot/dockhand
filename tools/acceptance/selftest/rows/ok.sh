# stages: selftest
# A row that harms nothing: its untracked file stays, its Next: lines
# run, and its envelope agrees with its exit.
setup() { echo mine >"$SELFTEST_CLONE/work.txt"; }
act() { dh_json status && dh hint; }
assert() { row_pass "harmless"; }
