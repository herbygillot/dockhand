# stages: quick full
# A5: the upgrade path. A copy of the person's database at the schema
# before the candidate's newest or earlier (ACCEPT_REAL_DB: unless set, the
# newest ~/.dockhand/dockhand.db.schema-N an earlier migration kept, else
# ~/.dockhand/dockhand.db; copied with SQLite's backup, which only reads
# it) is opened by the build before that schema, then by the candidate: the migration says so and keeps the copy
# of the old schema; status --all lists the same branches before and
# after; and the older build then refuses the database by name. Status
# reads the person's ports checkout (ACCEPT_REAL_TREE, else the stage's
# ACCEPT_PORTS_SOURCE), as it only reads. The database is opened read-only;
# where there's none, the row isn't run.

setup() {
	local schema newest previous
	schema=$ACCEPT_REPO/internal/store/sqlite/schema
	newest=$(ls "$schema" | sort | tail -1)
	previous=$((10#${newest%.sql} - 1))
	if [ -z "${ACCEPT_REAL_DB:-}" ]; then
		ACCEPT_REAL_DB=$HOME/.dockhand/dockhand.db
		# The newest copy kept at or before the schema before: a migration
		# keeps the schema it started from, so dhtest's went from 32 to 39
		# and kept 32's, where only a 38 was looked for (the rc4 full
		# stage). The build before migrates it the rest of the way first.
		local kept n best=0
		for kept in "$HOME"/.dockhand/dockhand.db.schema-*; do
			[ -f "$kept" ] || continue
			n=${kept##*.schema-}
			case "$n" in '' | *[!0-9]*) continue ;; esac
			[ "$((10#$n))" -le "$previous" ] && [ "$((10#$n))" -gt "$best" ] && best=$((10#$n)) && ACCEPT_REAL_DB=$kept
		done
	fi
	: "${ACCEPT_REAL_TREE:=${ACCEPT_PORTS_SOURCE:-$HOME/Source/macports-ports}}"
	# Without a database of the person's, there's nothing to migrate, and
	# the row touches nothing of theirs: SQLite would make an empty one
	# where it's asked to read a file that isn't there.
	if [ ! -f "$ACCEPT_REAL_DB" ]; then
		row_result "not run" "no database of yours to copy at $ACCEPT_REAL_DB; set ACCEPT_REAL_DB"
		return 0
	fi
	mkdir -p "$ROW_DIR/db" "$ROW_DIR/old" "$ROW_DIR/source"
	# The database is in WAL mode, which a read-only open can't read
	# without its -shm, nor make one (the rc3 full stage: "unable to open
	# database file"): it's copied with its -wal and -shm, and backed up
	# from the copy, which leaves the person's untouched.
	local part
	for part in "" -wal -shm; do
		[ ! -f "$ACCEPT_REAL_DB$part" ] || cp "$ACCEPT_REAL_DB$part" "$ROW_DIR/source/dockhand.db$part"
	done
	sqlite3 "$ROW_DIR/source/dockhand.db" ".backup '$ROW_DIR/db/dockhand.db'" || return 1
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

# a5_refused_live is whether a status was refused for processes working
# on the database, which the candidate does rather than migrate under them.
a5_refused_live() { jq -r '.error // ""' "$1" 2>/dev/null | grep -q 'would leave them unable to record what they do'; }

act() {
	a5_status "$ROW_DIR/old/dockhand" "$ROW_DIR/before.json" || :
	# The older build's status starts its automatic cleanup in the
	# background, and the candidate rightly won't migrate while that runs
	# (the rc1 full stage): it's waited for, up to two minutes, as a person
	# would, rather than read as nothing to migrate.
	local tries=0
	a5_status "$DH_BIN" "$ROW_DIR/after.json" || :
	while a5_refused_live "$ROW_DIR/after.json" && [ "$tries" -lt 12 ]; do
		sleep 10
		tries=$((tries + 1))
		a5_status "$DH_BIN" "$ROW_DIR/after.json" || :
	done
	a5_status "$ROW_DIR/old/dockhand" "$ROW_DIR/refused.json" || :
}

assert() {
	local names_before names_after
	names_before=$(jq -r '[.result.branches[]?.name] | sort | join(" ")' "$ROW_DIR/before.json" 2>/dev/null)
	names_after=$(jq -r '[.result.branches[]?.name] | sort | join(" ")' "$ROW_DIR/after.json" 2>/dev/null)
	if jq -r '.error // ""' "$ROW_DIR/before.json" 2>/dev/null | grep -q 'newer than this dockhand supports'; then
		row_result "not run" "nothing to migrate: $ACCEPT_REAL_DB is already at the candidate's schema, and no earlier copy was kept; set ACCEPT_REAL_DB to a database at the schema before"
		return
	fi
	if [ "$(jq -r .exit_code "$ROW_DIR/before.json" 2>/dev/null)" != 0 ]; then
		row_fail "the older build couldn't read the copy: $(jq -r '.error // empty' "$ROW_DIR/before.json") $(tail -2 "$ROW_DIR/before.json.err")"
		return
	fi
	if a5_refused_live "$ROW_DIR/after.json"; then
		row_fail "the candidate still refused to migrate after two minutes, for processes working on the database: $(jq -r .error "$ROW_DIR/after.json")"
		return
	fi
	if ! grep -q "^Migrated dockhand's database from schema" "$ROW_DIR/after.json.err"; then
		row_result "not run" "nothing to migrate: the copy is already at the candidate's schema; set ACCEPT_REAL_DB to a database at the schema before"
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
