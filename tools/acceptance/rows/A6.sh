# stages: quick full
# A6: dockhand backup of the stage's database while serve runs (#92). The
# copy is made into a directory, named by the minute, readable only by its
# owner; it opens with the same branches as the database; and a candidate
# run on it with DOCKHAND_DB leaves the database alone. A5 covers the copy
# of an older schema, which the candidate migrates while the original
# stays as it was.
act() {
	mkdir -p "$ROW_DIR/backups"
	dh_bg serve
	A6_SERVE=$DH_BG_PID A6_SERVE_LOG=$DH_BG_LOG
	wait_for_line "$DH_BG_LOG" 'leading|serve:' 60 || :
	dh_json backup "$ROW_DIR/backups" || :
	kill "$A6_SERVE" 2>/dev/null
	DH_BG_PID=$A6_SERVE DH_BG_LOG=$A6_SERVE_LOG dh_bg_wait || :
	A6_COPY=$(ls "$ROW_DIR"/backups/dockhand-*.db 2>/dev/null | head -1)
	"$DH_BIN" --json status --all 2>/dev/null | jq -r '[.result.branches[]?.name] | sort | join(",")' >"$ROW_DIR/branches.original" || :
	stat -f '%m %z' "${DOCKHAND_DB:?}" >"$ROW_DIR/original.before"
	[ -z "$A6_COPY" ] || DOCKHAND_DB=$A6_COPY "$DH_BIN" --json status --all 2>>"$ROW_DIR/out.log" | jq -r '[.result.branches[]?.name] | sort | join(",")' >"$ROW_DIR/branches.copy" || :
	stat -f '%m %z' "$DOCKHAND_DB" >"$ROW_DIR/original.after"
}
assert() {
	[ -n "${A6_COPY:-}" ] || { row_fail "backup made no copy in the directory: $(tail -3 "$ROW_DIR/out.log" | tr '\n' ';')"; return; }
	case "$(basename "$A6_COPY")" in
	dockhand-[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9][0-9][0-9].db) ;;
	*) row_fail "the copy isn't named by the minute: $(basename "$A6_COPY")"; return ;;
	esac
	[ "$(stat -f '%Lp' "$A6_COPY")" = 600 ] || { row_fail "the copy is mode $(stat -f '%Lp' "$A6_COPY"), not 600"; return; }
	[ "$(jq -r '.result.copy' "$DH_LAST_JSON")" = "$A6_COPY" ] || { row_fail "backup's --json names another copy: $(jq -c .result "$DH_LAST_JSON")"; return; }
	cmp -s "$ROW_DIR/branches.original" "$ROW_DIR/branches.copy" ||
		{ row_fail "the copy's branches differ: $(cat "$ROW_DIR/branches.copy") against $(cat "$ROW_DIR/branches.original")"; return; }
	cmp -s "$ROW_DIR/original.before" "$ROW_DIR/original.after" ||
		{ row_fail "a run on the copy changed the database"; return; }
	row_pass "backed up while serve ran, to $(basename "$A6_COPY") ($(jq -r '.result.bytes' "$DH_LAST_JSON") bytes), with the same branches"
}
