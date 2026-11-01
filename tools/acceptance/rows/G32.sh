# stages: quick full
# G32: a worksrcdir the new source lacks is said before its patches
# (7f009d2a, the outdated-ports session's note 20). angelscript 2.33.0,
# at the stage's pinned master, builds in "sdk/angelscript/projects/gnuc
# macosx", which 2.38.0's zip dropped: update angelscript 2.38.0 --plan
# says "! worksrcdir sdk/angelscript/projects/gnuc macosx/ isn't in
# 2.38.0's source" ahead of its patch's line, where it said only that
# patch-makefile.diff found no file to patch, and its JSON carries
# missing_worksrcdir. Fetches 2.38.0's zip, about 2 MB, from angelcode.com.
act() {
	dh update angelscript 2.38.0 --plan </dev/null || :
	dh_json update angelscript 2.38.0 --plan </dev/null || :
}
assert() {
	local json=$ROW_DIR/json/1.json said worksrcdir patch
	[ -f "$json" ] || { row_fail "update --plan didn't run"; return; }
	said=$(sed -n '/^\$ dockhand update angelscript 2.38.0 --plan$/,/^\[exit/p' "$ROW_DIR/out.log")
	if printf '%s\n' "$said" | tail -1 | matches -vF '[exit 0]'; then
		row_fail "update angelscript 2.38.0 --plan $(printf '%s' "$said" | tail -1): $(printf '%s\n' "$said" | sed 1d | head -3 | tr '\n' ' ')"
		return
	fi
	worksrcdir=$(printf '%s\n' "$said" | grep -n "^! worksrcdir sdk/angelscript/projects/gnuc macosx/\{0,1\} isn't in 2.38.0's source" | head -1 | cut -d: -f1)
	patch=$(printf '%s\n' "$said" | grep -n '^! patch patch-makefile.diff' | head -1 | cut -d: -f1)
	if [ -z "$worksrcdir" ]; then
		row_fail "update didn't say the worksrcdir 2.38.0's source lacks: $(printf '%s\n' "$said" | grep '^!' | head -3 | tr '\n' ' ')"
		return
	fi
	if [ -n "$patch" ] && [ "$patch" -lt "$worksrcdir" ]; then
		row_fail "the patch's line came before the worksrcdir's"
		return
	fi
	if [ -z "$(jq -r '.. | objects | .missing_worksrcdir? // empty' "$json" | head -1)" ]; then
		row_fail "update --json carried no missing_worksrcdir"
		return
	fi
	row_pass "$(printf '%s\n' "$said" | sed -n "${worksrcdir}p"), ahead of its patch's line"
}
