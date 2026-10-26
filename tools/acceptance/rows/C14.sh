# stages: quick full
# C14: update --hand-edit (v0.6.0, Then's item 3; aa67f61b, 4a4253ed). On a
# branch of the small Go port whose checksums name their first algorithm
# through a command, which update refuses as a calculated algorithm: the
# refusal names --hand-edit; update --hand-edit writes the attempt, the new
# version in the Portfile, saying the checksums it couldn't write; and
# status says the hand edit with the refusal, recorded as the person's.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

act() {
	C14_BRANCH=$(run_name c14-hand)
	dh_setup start "$C14_BRANCH" --port "$(port)" || return 0
	local dir
	dir=$("$DH_BIN" path "$C14_BRANCH") || return 0
	C14_PORTFILE=$(find "$dir" -path "*/$(port)/Portfile" | head -1)
	[ -n "$C14_PORTFILE" ] || return 0
	cp "$C14_PORTFILE" "$ROW_DIR/Portfile.before"
	# The first rmd160 is the checksums block's own: go.vendors' rows carry
	# sha256 and size alone.
	perl -0pi -e 's/\brmd160\b/[join {rmd 160} ""]/' "$C14_PORTFILE"
	allow_change "$C14_PORTFILE"
	dh_json update "$(port)" -b "$C14_BRANCH" || :
	dh update "$(port)" -b "$C14_BRANCH" --hand-edit || :
	dh_json status -b "$C14_BRANCH" || :
}

assert() {
	[ -n "${C14_PORTFILE:-}" ] || { row_fail "the branch's Portfile wasn't found"; return; }
	local refused=$ROW_DIR/json/1.json status=$ROW_DIR/json/2.json
	if [ "$(cat "$refused.exit")" = 0 ]; then
		row_fail "the update with a calculated checksum algorithm wasn't refused"
		return
	fi
	if ! sed -n "/^\\\$ dockhand --json update $(port) -b $C14_BRANCH\$/,/^\\\$ dockhand update/p" "$ROW_DIR/out.log" | matches -- '--hand-edit'; then
		row_fail "the refusal didn't name --hand-edit: $(jq -r '.error // empty' "$refused")"
		return
	fi
	if ! grep -q "^Wrote dockhand's attempt for you to finish by hand" "$ROW_DIR/out.log"; then
		row_fail "update --hand-edit wrote no attempt: $(sed -n "/^\\$ dockhand update $(port) -b $C14_BRANCH --hand-edit/,/^\\[exit/p" "$ROW_DIR/out.log" | tail -3 | tr '\n' ' ')"
		return
	fi
	if cmp -s <(grep -v '^checksums' "$ROW_DIR/Portfile.before") <(grep -v '^checksums' "$C14_PORTFILE"); then
		row_fail "the attempt left the Portfile's version as it was"
		return
	fi
	local words
	words=$(jq -r '.result.branches[0].hand_edits[0].refused // empty' "$status")
	case "$words" in
	*"calculated checksum algorithm"*) ;;
	*)
		row_fail "status didn't say the hand edit with its refusal: [$words]"
		return
		;;
	esac
	row_pass "refused naming --hand-edit; the attempt written; status says it: $words"
}
