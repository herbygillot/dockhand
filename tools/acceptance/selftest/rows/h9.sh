# stages: selftest
# Breaks H9: the person's own Tart home made, as a read-only command once did.
act() { mkdir -p "$SELFTEST_HOME/.tart/cache"; : >"$SELFTEST_HOME/.tart/cache/x"; }
assert() { row_pass; }
