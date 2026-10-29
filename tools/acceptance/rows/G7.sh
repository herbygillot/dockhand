# stages: quick full
# G7: dockhand move (v0.7.0): the small Go port moved to another
# category on a branch of its own, checked as the stage checks, tidied,
# and previewed for submission as a move. The check plans it as moved, not
# added; tidy makes one commit, "<port>: move to <category>", which the
# preview's title is.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

act() {
	local from
	from=$(git -C "${MACPORTS_TREE:?}" ls-tree -r --name-only HEAD 2>/dev/null | grep -E "^[^/]+/$(port)/Portfile$" | head -1)
	G7_CATEGORY=sysutils
	case "$from" in sysutils/*) G7_CATEGORY=devel ;; esac
	dh_setup move "$(port)" "$G7_CATEGORY" --new || return 0
	G7_BRANCH=$(own_branch)
	[ -n "$G7_BRANCH" ] || { row_fail "move --new made no branch"; return 0; }
	dh_json check -b "$G7_BRANCH" || return 0
	dh_json tidy -b "$G7_BRANCH" -y || return 0
	dh submit -b "$G7_BRANCH" --plan || :
	git -C "$("$DH_BIN" path "$G7_BRANCH")" log -1 --format=%s >"$ROW_DIR/subject" 2>/dev/null || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	local step file
	for step in "1 check" "2 tidy"; do
		set -- $step
		file=$ROW_DIR/json/$1.json
		[ "$(cat "$file.exit" 2>/dev/null)" = 0 ] || { row_fail "$2 of the moved $(port) exited $(cat "$file.exit" 2>/dev/null): $(jq -r '.error // empty' "$file" 2>/dev/null)"; return; }
	done
	local check=$ROW_DIR/json/1.json target
	target=$(jq -c --arg name "$(port)" '.result.plan.targets[]? | select(.name == $name)' "$check")
	printf '%s' "$target" | jq -e '(.moved_from | strings) and (.added | not)' >/dev/null ||
		{ row_fail "the check didn't plan $(port) as moved: ${target:-no such target}"; return; }
	local subject
	subject=$(cat "$ROW_DIR/subject" 2>/dev/null)
	[ "$subject" = "$(port): move to $G7_CATEGORY" ] || { row_fail "tidy's commit isn't the move's: $subject"; return; }
	if sed -n '/^\$ dockhand submit/,/^\[exit/p' "$ROW_DIR/out.log" | matches -F "Title    $subject"; then
		row_pass "moved to $G7_CATEGORY, checked as a move, tidied as \"$subject\", and previewed under that title"
	elif sed -n '/^\$ dockhand submit/,/^\[exit/p' "$ROW_DIR/out.log" | matches -i fork; then
		row_pass "moved to $G7_CATEGORY, checked as a move, and tidied as \"$subject\"; the preview stopped for want of a GitHub fork, which the quick stage hasn't"
	else
		row_fail "submit's preview didn't title it \"$subject\": $(sed -n '/^\$ dockhand submit/,/^\[exit/p' "$ROW_DIR/out.log" | tail -3 | tr '\n' ';')"
	fi
}
