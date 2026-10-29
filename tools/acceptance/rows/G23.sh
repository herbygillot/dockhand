# stages: quick full
# G23: dockhand usage with no checkout (v0.7.0). From an empty directory,
# with no MACPORTS_TREE, dockhand usage and each of its topics, as
# docs/usage names them, print their page and exit 0: the guide is in the
# binary, and reading it needs no ports tree.
act() {
	local dir topic
	dir=$(mktemp -d "$ROW_DIR/nowhere.XXXX")
	: >"$ROW_DIR/usage.failed"
	for topic in "" $(cd "${ACCEPT_REPO:?}/docs/usage" && ls ./*.md | sed 's|^\./||; s|\.md$||'); do
		[ "$topic" = setup ] && continue
		printf '$ dockhand usage %s (no checkout)\n' "$topic" >>"$ROW_DIR/out.log"
		if ! (cd "$dir" && env -u MACPORTS_TREE "$DH_BIN" usage $topic) >"$ROW_DIR/usage.out" 2>&1 </dev/null || [ ! -s "$ROW_DIR/usage.out" ]; then
			printf '%s\n' "${topic:-usage}" >>"$ROW_DIR/usage.failed"
		fi
		head -3 "$ROW_DIR/usage.out" >>"$ROW_DIR/out.log"
	done
}

assert() {
	if [ -s "$ROW_DIR/usage.failed" ]; then
		row_fail "with no checkout, these printed nothing or failed: $(tr '\n' ' ' <"$ROW_DIR/usage.failed")"
		return
	fi
	row_pass "dockhand usage and each topic printed its page with no checkout"
}
