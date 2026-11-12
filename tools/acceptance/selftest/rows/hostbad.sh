# stages: quick
# A row that fails once the host has turned bad during it: the runner reads
# the host's checks again and says the row was blocked by the host (#226).
act() { : >"$FAKE_DH_STATE/tart-hangs"; }
assert() { row_fail "a step timed out"; }
