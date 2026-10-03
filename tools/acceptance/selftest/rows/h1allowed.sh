# stages: selftest
# The same change, which the row says it makes: no harm.
setup() { echo mine >"$SELFTEST_CLONE/work.txt"; }
act() { allow_change work.txt; echo changed >"$SELFTEST_CLONE/work.txt"; }
assert() { row_pass; }
