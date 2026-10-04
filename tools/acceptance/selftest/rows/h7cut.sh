# stages: selftest
# A serve the runner cut leaves one check stopped and one queued: the
# runner cancels both before H7 reads the queue, and the row isn't run,
# with what was cut.
act() {
	echo '{"runs": [{"name": "check-8", "state": "running", "stopped": true}, {"name": "check-9", "state": "queued"}]}' >"$FAKE_DH_STATE/queue.json"
	cut_command "dockhand serve --drain, still building rust-1.91.0"
}
assert() { row_pass; }
