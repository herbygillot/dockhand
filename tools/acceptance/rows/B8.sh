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
	while IFS= read -r url; do
		[ -n "$url" ] || continue
		dh_json create "$url" --new || continue
		name=$(jq -r '.result.branch.name // empty' "$DH_LAST_JSON")
		dh check -b "$name" || :
		# submit previews commits, so the new port is tidied first, as B4
		# tidies (the rc6 full stage).
		dh tidy -b "$name" -y || :
		dh_json submit -b "$name" --plan || :
	done <"$ROW_DIR/projects"
	# A module before go 1.17 whose go.mod leaves out a module its build
	# reads is refused, naming it: countdown 1.5.0, at go 1.14, needs
	# rivo/uniseg, which only its go.sum has (the rc6 full stage, batch 108).
	dh_json create "${ACCEPT_B8_UNPRUNED:-https://github.com/antonmedv/countdown}" --new || :
	B8_UNPRUNED=$DH_LAST_JSON
}
assert() {
	grep -q unconfirmed "$ROW_DIR/out.log" || { row_fail "no guess was marked unconfirmed"; return; }
	if [ -n "${B8_UNPRUNED:-}" ] && ! jq -r '.error // ""' "$B8_UNPRUNED" | grep -q 'go.sum has the source of .*rivo/uniseg'; then
		row_fail "create didn't refuse countdown's go.vendors, naming rivo/uniseg: $(jq -r '.error // "it succeeded"' "$B8_UNPRUNED")"
		return
	fi
	judged "both new ports built, and their previews read right"
}
