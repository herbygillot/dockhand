# stages: quick full
# G27: refusals and last lines that name the command meant (v0.8.0):
#  - throwing a branch away is said where it's asked (e45dcd92, the
#    outdated-ports session's note 24): clean of an open branch is
#    refused, exit 1, ending "dockhand clean --delete <branch> then forgets
#    it for good"; archive's last line says the same; and a clean that
#    removes nothing while the archived branch keeps its Git branch says
#    so, naming clean --delete, where it said "Nothing to remove" alone.
#    That plain clean is the quick stage's alone: its line counts every
#    archived branch the database keeps, which a full stage's database
#    has hundreds of (dhtest's 954 at rc4's full stage), and it would
#    remove what earlier rows there left;
#  - logs <check> -p <port> is refused, exit 1, naming logs <check> <port>,
#    since -p chooses a branch (02bd5e81, v0.8.0's upgrade notes).
setup() {
	G27_BRANCH=$(run_name g27)
	dh_setup start "$G27_BRANCH" </dev/null || return 1
	allow_ref_gone "*/$G27_BRANCH"
}
act() {
	[ -n "${G27_BRANCH:-}" ] || return 0
	dh_json clean "$G27_BRANCH" -y </dev/null || :
	dh archive "$G27_BRANCH" </dev/null || :
	[ "${ACCEPT_STAGE:-}" = full ] || dh clean -y </dev/null || :
	dh_json logs check-1 -p "${ACCEPT_GO_PORT:?}" </dev/null || :
}
assert() {
	local open=$ROW_DIR/json/1.json logs=$ROW_DIR/json/2.json words
	[ -f "$logs" ] || { row_fail "a step didn't run"; return; }
	words="dockhand clean --delete $G27_BRANCH then forgets it for good"
	if [ "$(cat "$open.exit")" != 1 ]; then
		row_fail "clean of the open branch exited $(cat "$open.exit"), not 1"
		return
	fi
	if ! jq -r '.error // empty' "$open" | matches -F "$words"; then
		row_fail "clean's refusal of the open branch didn't end naming clean --delete: $(jq -r '.error // empty' "$open")"
		return
	fi
	if ! sed -n "/^\\\$ dockhand archive $G27_BRANCH\$/,/^\\[exit/p" "$ROW_DIR/out.log" | tr '\n' ' ' | sed 's/  */ /g' | matches -F "$words."; then
		row_fail "archive's last line didn't name clean --delete: $(sed -n "/^\\\$ dockhand archive $G27_BRANCH\$/,/^\\[exit/p" "$ROW_DIR/out.log" | tail -3 | tr '\n' ' ')"
		return
	fi
	if [ "${ACCEPT_STAGE:-}" != full ] && ! sed -n '/^\$ dockhand clean -y$/,/^\[exit/p' "$ROW_DIR/out.log" | tr '\n' ' ' | sed 's/  */ /g' | matches -F "1 archived branch, $G27_BRANCH, keeps its Git branch for archive --undo; $words."; then
		row_fail "the clean that removed nothing didn't say the archived branch it keeps: $(sed -n '/^\$ dockhand clean -y$/,/^\[exit/p' "$ROW_DIR/out.log" | sed 1d | tr '\n' ' ')"
		return
	fi
	if [ "$(cat "$logs.exit")" != 1 ] || ! jq -r '.error // empty' "$logs" | matches -F "dockhand logs check-1 ${ACCEPT_GO_PORT}"; then
		row_fail "logs check-1 -p ${ACCEPT_GO_PORT} exited $(cat "$logs.exit"), not refused naming logs check-1 ${ACCEPT_GO_PORT}: $(jq -r '.error // empty' "$logs")"
		return
	fi
	local named="archive and a clean removing nothing"
	[ "${ACCEPT_STAGE:-}" != full ] || named="archive (the plain clean is the quick stage's)"
	row_pass "clean refused the open branch: $(jq -r '.error' "$open"); $named named clean --delete; logs -p refused: $(jq -r '.error' "$logs")"
}
