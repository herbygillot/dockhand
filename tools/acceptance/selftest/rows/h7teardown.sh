# stages: selftest
# Breaks H7 past its teardown: a check it allowed itself to leave running
# while it acted is still running once it's torn down.
act() {
	allow_running "run check-7"
	echo '{"runs": [{"name": "check-7", "state": "running"}]}' >"$FAKE_DH_STATE/queue.json"
}
assert() { row_pass; }
