# stages: selftest
# Breaks H2: something pushed to the fork that the row didn't say.
act() { git -C "$SELFTEST_CLONE" push -q fork HEAD:refs/heads/stray; }
assert() { row_pass; }
