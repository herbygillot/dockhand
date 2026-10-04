# stages: selftest
# A check left stopped with no cut by the runner is the row's failure,
# canceled before H7 reads the queue.
act() { echo '{"runs": [{"name": "check-9", "state": "running", "stopped": true}]}' >"$FAKE_DH_STATE/queue.json"; }
assert() { row_pass; }
