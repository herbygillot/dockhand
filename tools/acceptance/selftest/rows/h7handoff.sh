# stages: selftest
# A check a row hands to a later half of itself, as D-I5's first half
# leaves its check to the reboot, is allowed past teardown (#234).
act() {
	allow_handoff "run check-7"
	echo '{"runs": [{"name": "check-7", "state": "running"}]}' >"$FAKE_DH_STATE/queue.json"
	after_result "reboot now"
}
assert() { row_pass; }
