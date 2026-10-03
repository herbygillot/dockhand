# stages: quick full
# A5: the upgrade path. A copy of the person's database at the schema
# before the candidate's newest (ACCEPT_REAL_DB: unless set, the
# ~/.dockhand/dockhand.db.schema-N an earlier migration kept, else
# ~/.dockhand/dockhand.db; copied with SQLite's backup, which only reads
# it) is opened by the build before that schema, then by the candidate: the migration says so and keeps the copy
# of the old schema; status --all lists the same branches before and
# after; and the older build then refuses the database by name. Status
# reads the person's ports checkout (ACCEPT_REAL_TREE), as it only reads.

setup() {
	local schema newest previous
	schema=$ACCEPT_REPO/internal/store/sqlite/schema
	newest=$(ls "$schema" | sort | tail -1)
	previous=$((10#${newest%.sql} - 1))
	if [ -z "${ACCEPT_REAL_DB:-}" ]; then
		ACCEPT_REAL_DB=$HOME/.dockhand/dockhand.db
		[ -f "$HOME/.dockhand/dockhand.db.schema-$previous" ] && ACCEPT_REAL_DB=$HOME/.dockhand/dockhand.db.schema-$previous
	fi
	: "${ACCEPT_REAL_TREE:=$HOME/Source/macports-ports}"
	mkdir -p "$ROW_DIR/db" "$ROW_DIR/old"
	sqlite3 "$ACCEPT_REAL_DB" ".backup '$ROW_DIR/db/dockhand.db'" || return 1
	# The build before the newest schema: the parent of the commit that
	# added it.
	A5_OLD_REV=$(git -C "$ACCEPT_REPO" log -1 --format=%H -- "internal/store/sqlite/schema/$newest")^
	git -C "$ACCEPT_REPO" archive "$A5_OLD_REV" | tar -x -C "$ROW_DIR/old" || return 1
	(cd "$ROW_DIR/old" && GOFLAGS=-mod=vendor go build -o "$ROW_DIR/old/dockhand" ./cmd/dockhand) >"$ROW_DIR/old-build.log" 2>&1
}

a5_status() {
	local bin=$1 out=$2
	DOCKHAND_DB="$ROW_DIR/db/dockhand.db" "$bin" --json --tree "$ACCEPT_REAL_TREE" status --all >"$out" 2>"$out.err"
}

act() {
	a5_status "$ROW_DIR/old/dockhand" "$ROW_DIR/before.json" || :
	a5_status "$DH_BIN" "$ROW_DIR/after.json" || :
	a5_status "$ROW_DIR/old/dockhand" "$ROW_DIR/refused.json" || :
}

assert() {
	local names_before names_after
	names_before=$(jq -r '[.result.branches[]?.name] | sort | join(" ")' "$ROW_DIR/before.json" 2>/dev/null)
	names_after=$(jq -r '[.result.branches[]?.name] | sort | join(" ")' "$ROW_DIR/after.json" 2>/dev/null)
	if jq -r '.error // ""' "$ROW_DIR/before.json" 2>/dev/null | grep -q 'newer than this dockhand supports'; then
		row_known "nothing to migrate: $ACCEPT_REAL_DB is already at the candidate's schema, and no earlier copy was kept"
		return
	fi
	if [ "$(jq -r .exit_code "$ROW_DIR/before.json" 2>/dev/null)" != 0 ]; then
		row_fail "the older build couldn't read the copy: $(jq -r '.error // empty' "$ROW_DIR/before.json") $(tail -2 "$ROW_DIR/before.json.err")"
		return
	fi
	if ! grep -q "^Migrated dockhand's database from schema" "$ROW_DIR/after.json.err"; then
		row_known "nothing to migrate: the copy is already at the candidate's schema"
		return
	fi
	if ! ls "$ROW_DIR/db"/dockhand.db.schema-* >/dev/null 2>&1; then
		row_fail "the migration kept no copy of the old schema"
		return
	fi
	if [ "$names_before" != "$names_after" ]; then
		row_fail "status --all differs: before [$names_before], after [$names_after]"
		return
	fi
	if [ "$(jq -r .exit_code "$ROW_DIR/refused.json" 2>/dev/null)" = 0 ] || ! jq -r .error "$ROW_DIR/refused.json" | grep -q "newer than this dockhand supports"; then
		row_fail "the older build didn't refuse the migrated database by name: $(jq -r .error "$ROW_DIR/refused.json" 2>/dev/null)"
		return
	fi
	if grep -q '^panic:' "$ROW_DIR"/*.err; then
		row_fail "a build panicked"
		return
	fi
	row_pass "migrated with a copy kept; status --all the same before and after; the older build refuses it by name"
}
