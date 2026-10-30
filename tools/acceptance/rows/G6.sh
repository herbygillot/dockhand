# stages: quick full
# G6: dockhand rename (v0.7.0): the small Go port renamed on a branch of
# its own to <it>-renamed, then updated by its new name, checked as the
# stage checks, tidied, and previewed for submission as a rename. The
# check plans the renamed port as renamed, not added, so lint takes it
# without --nitpick, and builds no stub; tidy makes one commit, "<new>:
# rename from <port>, update to <version>", which the preview's title is.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
renamed() { printf '%s-renamed' "$(port)"; }

act() {
	dh_setup rename "$(port)" "$(renamed)" --new || return 0
	G6_BRANCH=$(own_branch)
	[ -n "$G6_BRANCH" ] || { row_fail "rename --new made no branch"; return 0; }
	# The lines rename lists under "Uses ${name}" read the new name now;
	# the port's binary is still its old name's, so the row edits them as
	# the person would, where destroot's "xinstall …/${name}" failed
	# (rc1's quick stage).
	local portfile line
	portfile=$(ls "$("$DH_BIN" path "$G6_BRANCH")"/*/"$(renamed)"/Portfile 2>/dev/null | head -1)
	sed -n '/Uses \${name}, which now reads/,/^ \{0,4\}[^ ]/p' "$ROW_DIR/out.log" | sed -n 's/^      line \([0-9][0-9]*\): .*/\1/p' | while IFS= read -r line; do
		sed -i '' "${line}s/\\\${name}/$(port)/g; ${line}s/\\\${subport}/$(port)/g" "$portfile"
	done
	dh_json update "$(renamed)" -b "$G6_BRANCH" || return 0
	dh_json check -b "$G6_BRANCH" || return 0
	dh_json tidy -b "$G6_BRANCH" -y || return 0
	dh submit -b "$G6_BRANCH" --plan || :
	git -C "$("$DH_BIN" path "$G6_BRANCH")" log -1 --format=%s >"$ROW_DIR/subject" 2>/dev/null || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	local step file
	for step in "1 update" "2 check" "3 tidy"; do
		set -- $step
		file=$ROW_DIR/json/$1.json
		[ "$(cat "$file.exit" 2>/dev/null)" = 0 ] || { row_fail "$2 of $(renamed) exited $(cat "$file.exit" 2>/dev/null): $(jq -r '.error // empty' "$file" 2>/dev/null)"; return; }
	done
	local check=$ROW_DIR/json/2.json target
	target=$(jq -c --arg name "$(renamed)" '.result.plan.targets[]? | select(.name == $name)' "$check")
	[ -n "$target" ] || { row_fail "the check planned no $(renamed): $(jq -c '[.result.plan.targets[]?.name]' "$check")"; return; }
	printf '%s' "$target" | jq -e '.moved_from | strings | endswith("/'"$(port)"'")' >/dev/null ||
		{ row_fail "the check didn't plan $(renamed) as renamed from $(port): $target"; return; }
	printf '%s' "$target" | jq -e '.added | not' >/dev/null || { row_fail "the check planned $(renamed) as a new port, linted with --nitpick"; return; }
	jq -e --arg old "$(port)" '[.result.plan.targets[]?.name] | index($old) | not' "$check" >/dev/null ||
		{ row_fail "the check planned the stub, $(port), to build"; return; }
	local subject
	subject=$(cat "$ROW_DIR/subject" 2>/dev/null)
	printf '%s' "$subject" | matches -E "^$(renamed): rename from $(port), update to " ||
		{ row_fail "tidy's commit isn't the rename's: $subject"; return; }
	if sed -n '/^\$ dockhand submit/,/^\[exit/p' "$ROW_DIR/out.log" | matches -F "Title    $subject"; then
		row_pass "renamed, updated, checked as a rename with no stub built, tidied as \"$subject\", and previewed under that title"
	elif sed -n '/^\$ dockhand submit/,/^\[exit/p' "$ROW_DIR/out.log" | matches -i fork; then
		row_pass "renamed, updated, checked as a rename with no stub built, and tidied as \"$subject\"; the preview stopped for want of a GitHub fork, which the quick stage hasn't"
	else
		row_fail "submit's preview didn't title it \"$subject\": $(sed -n '/^\$ dockhand submit/,/^\[exit/p' "$ROW_DIR/out.log" | tail -3 | tr '\n' ';')"
	fi
}
