# stages: selftest
# A fork remote H2 can't read leaves the row unobserved, never a pass: it's
# blocked and joins the rerun list (#231).
setup() { git -C "$SELFTEST_CLONE" remote set-url fork /nonexistent/fork.git; }
assert() { row_pass "it says it passed"; }
