# stages: selftest
# A row during which a pause is asked for: the run stops before the next
# row (#225).
act() { : >"$ACCEPT_STATE/pause"; }
assert() { row_pass "a pause was asked for"; }
