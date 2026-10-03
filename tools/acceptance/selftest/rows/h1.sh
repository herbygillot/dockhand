# stages: selftest
# Breaks H1: a person's untracked file is overwritten.
setup() { echo mine >"$SELFTEST_CLONE/work.txt"; }
act() { echo lost >"$SELFTEST_CLONE/work.txt"; }
assert() { row_pass; }
