# stages: selftest
# Breaks H7: a check left running.
act() { echo '{"runs": [{"name": "check-7", "state": "running"}]}' >"$FAKE_DH_STATE/queue.json"; }
assert() { row_pass; }
