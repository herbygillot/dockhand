# stages: selftest
# Breaks H5: status says a head Git doesn't have.
setup() {
	printf '{"branches": [{"name": "master", "git_branch": "master", "head": "%s", "worktree": "", "edited": []}]}\n' 0000000000000000000000000000000000000000 >"$FAKE_DH_STATE/status.json"
}
assert() { row_pass; }
