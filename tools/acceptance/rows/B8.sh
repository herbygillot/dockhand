# stages: full
# B8: create for a new Go port and a new Rust port, through to a test PR
# unless the person picks a real new port. Guesses are marked
# unconfirmed, the port builds, and the preview reads right.
# prs: new-go test
# prs: new-rust test
act() {
	host_only "a new port through to a pull request" || return 0
	checkpoint "name a Go project URL and a Rust project URL for new ports in $ROW_DIR/projects, one a line" || return 0
	local url name
	while IFS= read -r url; do
		[ -n "$url" ] || continue
		dh_json create "$url" --new || continue
		name=$(jq -r '.result.branch.name // empty' "$DH_LAST_JSON")
		dh check -b "$name" || :
		dh_json submit -b "$name" --plan || :
	done <"$ROW_DIR/projects"
}
assert() {
	grep -q unconfirmed "$ROW_DIR/out.log" || { row_fail "no guess was marked unconfirmed"; return; }
	judged "both new ports built, and their previews read right"
}
