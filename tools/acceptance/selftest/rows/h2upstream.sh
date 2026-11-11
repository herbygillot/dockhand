# stages: selftest
# Breaks H2: something pushed to the quick stage's upstream, a local bare
# repository a push lands in (#147).
act() { git -C "$SELFTEST_CLONE" push -q upstream HEAD:refs/heads/dockhand/jq-1.8.1; }
assert() { row_pass; }
