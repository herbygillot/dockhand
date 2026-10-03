# stages: selftest
# Breaks H8: the envelope's exit_code isn't the process's.
setup() { echo 3 >"$FAKE_DH_STATE/json_exit"; }
act() { dh_json update jq; }
assert() { row_pass; }
