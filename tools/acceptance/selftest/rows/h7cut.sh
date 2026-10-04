# stages: selftest
# A serve the runner cut leaves its check stopped: the runner cancels it
# before H7 reads the queue, and the row isn't run, with what was cut.
act() {
	echo '{"runs": [{"name": "check-8", "state": "running", "stopped": true}]}' >"$FAKE_DH_STATE/queue.json"
	cut_command "dockhand serve --drain, still building rust-1.91.0"
}
assert() { row_pass; }
