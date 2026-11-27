# stages: full
# B8: create for a new Go port and a new Rust port, through to a test PR
# unless the person picks a real new port. Guesses are marked
# unconfirmed, the port builds, and the preview reads right.
# prs: new-go test
# prs: new-rust test
act() {
	host_only "a new port through to a pull request" || return 0
	checkpoint "name a Go project URL whose go.mod says go 1.17 or later, and a Rust project URL, for new ports in $ROW_DIR/projects, one a line" || return 0
	local url name
	rm -f "$ROW_DIR/checks"
	while IFS= read -r url; do
		[ -n "$url" ] || continue
		local status=0
		dh_json create "$url" --new || status=$?
		if [ "$status" -ne 0 ]; then
			printf '%s\t%s\n' "create $url" "$status" >>"$ROW_DIR/checks"
			continue
		fi
		name=$(jq -r '.result.branch.name // empty' "$DH_LAST_JSON")
		# --fresh: an earlier run's check of the same change on the same
		# master would be reused, and nothing built (#227).
		# Each check's exit is what the assert reads: a check that
		# planned nothing and exited 1 was recorded as built (the rc2
		# full stage, walk's findBinary gzip).
		status=0
		dh check -b "$name" --fresh || status=$?
		printf '%s\t%s\n' "$name" "$status" >>"$ROW_DIR/checks"
		# submit previews commits, so the new port is tidied first, as B4
		# tidies (the rc6 full stage).
		dh tidy -b "$name" -y || :
		dh_json submit -b "$name" --plan || :
	done <"$ROW_DIR/projects"
	# A module before go 1.17 whose go.mod leaves out a module its build
	# reads can't have its go.vendors made, naming it: countdown 1.5.0, at
	# go 1.14, needs rivo/uniseg, which only its go.sum has (the rc6 full
	# stage, batch 108). create drafts the port and says why its checksums
	# aren't filled in, as for its other checksum failures, and the check
	# stops it before submit (the rc8 full stage).
	dh_json create "${ACCEPT_B8_UNPRUNED:-https://github.com/antonmedv/countdown}" --new || :
	B8_UNPRUNED=$DH_LAST_JSON
}
assert() {
	grep -q unconfirmed "$ROW_DIR/out.log" || { row_fail "no guess was marked unconfirmed"; return; }
	if [ ! -s "$ROW_DIR/checks" ]; then
		row_fail "no project was named for a new port"
		return
	fi
	local failed
	failed=$(awk -F '\t' '$2 != 0 { printf "%s%s (exit %s)", sep, $1, $2; sep = ", " }' "$ROW_DIR/checks")
	if [ -n "$failed" ]; then
		row_fail "a new port's create or check didn't pass: $failed"
		return
	fi
	if [ -n "${B8_UNPRUNED:-}" ] && ! jq -r '.result.checksums_problem // ""' "$B8_UNPRUNED" | matches 'go.sum has the source of .*rivo/uniseg'; then
		row_fail "create didn't say countdown's go.vendors needs rivo/uniseg: $(jq -r '.result.checksums_problem // .error // "it said nothing"' "$B8_UNPRUNED")"
		return
	fi
	judged "both new ports built, and their previews read right"
}
