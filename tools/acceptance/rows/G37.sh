# stages: full
# G37: a check carried over a change that builds nothing differently (the
# omnidiff session's note 8). A Tart check of the day's Go port's update,
# then its maintainers edited, openmaintainer added, or taken away where
# the port has it, and tidied: the check after it boots nothing, reusing
# the first check's builds, its port's result saying what changed since,
# the maintainers, and that lint ran again on the Portfile on this Mac,
# or why its lines are the earlier Portfile's; and submit's preview reads
# that check as written, its Tested on saying the same, with no block
# for a check of other files.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	host_only "a Tart check" || return 0
	local dir portfile
	dh_setup update "$(port)" --new || return 0
	G37_BRANCH=$(own_branch)
	dh check -b "$G37_BRANCH" || return 0
	dir=$("$DH_BIN" path "$G37_BRANCH") || { row_fail "dockhand path $G37_BRANCH found no worktree"; return 0; }
	portfile=$(find "$dir" -mindepth 3 -maxdepth 3 -path "*/$(port)/Portfile" | head -1)
	[ -f "$portfile" ] || { row_fail "no Portfile of $(port) in $dir"; return 0; }
	allow_change "$dir/*"
	if grep -qw openmaintainer "$portfile"; then
		perl -pi -e 's/\s+openmaintainer\b//' "$portfile"
	else
		perl -pi -e 's/^(maintainers\s+\S.*?)(\s*\\)?$/$1 openmaintainer$2/ if !$done++ && /^maintainers\s/' "$portfile"
	fi
	git -C "$dir" diff --stat >>"$ROW_DIR/out.log" 2>&1
	dh tidy -b "$G37_BRANCH" -y || return 0
	dh_json check -b "$G37_BRANCH" || :
	dh_json submit -b "$G37_BRANCH" --plan || :
}
assert() {
	local check=$ROW_DIR/json/1.json preview=$ROW_DIR/json/2.json carried
	[ -f "$check" ] || { row_fail "the check after the maintainers edit didn't run"; return; }
	[ "$(cat "$check.exit" 2>/dev/null)" = 0 ] || { row_fail "the check after the maintainers edit failed: $(jq -r '.error // empty' "$check")"; return; }
	if jq -e '[.result.targets[]?.results[]? | select(.excluded != true) | select((.reused_from // "") == "")] | length > 0' "$check" >/dev/null; then
		row_fail "the check after an edit that builds nothing differently built: $(jq -c '[.result.targets[]? | {port: .name, results: [.results[]? | {outcome, reused_from}]}]' "$check" | cut -c1-300)"
		return
	fi
	carried=$(jq -c --arg port "$(port)" '[.result.targets[]? | select(.name == $port) | .results[]?.carried // empty] | first // empty' "$check")
	[ -n "$carried" ] || { row_fail "$(port)'s reused result doesn't say it was carried over the edit"; return; }
	printf '%s' "$carried" | jq -e '[.ports[]?.options[]?] | index("maintainers")' >/dev/null ||
		{ row_fail "$(port)'s carried result doesn't name its maintainers: $carried"; return; }
	printf '%s' "$carried" | jq -e '.linted == true or (.lint_kept // "") != ""' >/dev/null ||
		{ row_fail "$(port)'s carried result says nothing of lint: $carried"; return; }
	[ -f "$preview" ] || { row_fail "submit --plan didn't run"; return; }
	if jq -r '.result.blocking[]? // empty' "$preview" | matches -i 'no completed check covers'; then
		row_fail "submit's preview didn't read the carried check: $(jq -r '.result.blocking[]?' "$preview" | head -2 | tr '\n' ' ')"
		return
	fi
	jq -r '.result.body // empty' "$preview" | matches -E "reused check-[0-9]+'s builds.*which builds nothing differently" ||
		{ row_fail "submit's preview's Tested on doesn't say what changed since the reused builds"; return; }
	row_pass "the check after the maintainers edit booted nothing, $(port)'s result carried ($(printf '%s' "$carried" | jq -r 'if .linted then "lint ran again here" else "lint: " + .lint_kept end')), and submit's preview said it: $(jq -r '.result.body' "$preview" | grep -oE "check-[0-9]+ reused check-[0-9]+'s builds[^.]*" | head -1)"
}
