# stages: quick full
# D-T1: a kept archive altered by one byte. A check that would install it
# catches it by its digest, and builds the port rather than install it,
# saying so ("the archive kept of X isn't the one it was kept as").
#
# A kept archive is given to a guest only as a dependency of what it
# builds, reused from an earlier check. So the row revbumps a dependency,
# ACCEPT_LIB_PORT, and a port that depends on it, ACCEPT_LIB_DEPENDENT, in
# one branch, checks both, which keeps the dependency's archive, alters
# that one archive by a byte, edits only the dependent, and checks again:
# the dependency's result is reused, its archive would be given to the
# guest, and the digest must stop it. The quick stage's ports depend on
# none, and it doesn't run this; the engine's
# TestTheGuestInstallsWhatABuildNeedsFromItsKeptArchive alters one there.
#
# Only that archive is altered, its bytes kept first, and put back after
# the check and again in teardown, however act ended; assert then finds
# every kept archive whole by its digest. rc8's altered all 47 and put none
# back, rust's and cargo's among them, which MacPorts has no macOS 27
# builds of, so every later Rust check would have built rust again (the
# rc8 full stage).

# dt1_archives is where the stage's dockhand keeps archives.
dt1_archives() { printf '%s/archives' "$(dirname "${DOCKHAND_DB:?}")"; }

# dt1_restore puts the altered archive back as it was kept, whether it's
# where it was or dockhand set it aside as <file>.altered.
dt1_restore() {
	[ -s "$ROW_DIR/dt1.path" ] && [ -f "$ROW_DIR/dt1.original" ] || return 0
	local path
	path=$(cat "$ROW_DIR/dt1.path")
	cp -p "$ROW_DIR/dt1.original" "$path" && rm -f "$path.altered"
}
. "${ROW_LIB:?}/fault.sh"
act() {
	if [ "${ACCEPT_STAGE:-}" = quick ]; then
		row_result "not run" "the stage's ports depend on no kept archive, so none is given to a guest; the engine's test covers an altered one"
		return 0
	fi
	if [ -z "${ACCEPT_LIB_PORT:-}" ] || [ -z "${ACCEPT_LIB_DEPENDENT:-}" ]; then
		row_result "not run" "set ACCEPT_LIB_PORT to a small library port and ACCEPT_LIB_DEPENDENT to a small port that depends on it"
		return 0
	fi
	dh_setup revbump "$ACCEPT_LIB_PORT" "$ACCEPT_LIB_DEPENDENT" --branch "$(run_name dt1-tamper)" --subject "rebuild to test a tampered archive" || return 0
	dh_setup check -b "$(run_name dt1-tamper)" || return 0
	local digest path dir
	# The library's newest kept archive, by its record.
	digest=$(sqlite3 "file:${DOCKHAND_DB}?mode=ro" "SELECT digest FROM archives WHERE name LIKE '$ACCEPT_LIB_PORT-%' ORDER BY kept_at DESC LIMIT 1" 2>>"$ROW_DIR/out.log")
	path=$(find "$(dt1_archives)" -type f -name "${digest#sha256:}" 2>/dev/null | head -1)
	DT1_ALTERED=0
	# None kept, as where cleanup has forgotten it, is said plainly: the
	# row has nothing to alter (the rc9 full stage).
	if [ -z "$path" ]; then
		row_result "not run" "no kept archive of $ACCEPT_LIB_PORT to alter: its dependent's check gave the guest none, or cleanup has forgotten it"
		return 0
	fi
	if [ -n "$path" ]; then
		cp -p "$path" "$ROW_DIR/dt1.original" && printf '%s\n' "$path" >"$ROW_DIR/dt1.path" || return 0
		fault_flip "$path"
		DT1_ALTERED=1
	fi
	dir=$("$DH_BIN" path "$(run_name dt1-tamper)") || return 0
	printf '\n# rebuilt with its dependency reused, to test a tampered archive\n' >>"$dir/$(dh_quiet --json status "$(run_name dt1-tamper)" | jq -r --arg p "$ACCEPT_LIB_DEPENDENT" '.result.branches[0].directories[] | select(endswith("/" + $p))')/Portfile"
	dh_json check -b "$(run_name dt1-tamper)" || :
	# The check's events, as its record keeps them, read from the stage's
	# own database, opened read-only.
	sqlite3 "file:${DOCKHAND_DB}?mode=ro" "SELECT message FROM events WHERE kind = 'archive.altered'" >>"$ROW_DIR/out.log" 2>&1 || :
	dt1_restore
}
teardown() {
	dt1_restore
	return 0
}
assert() {
	# Every kept archive is whole, the altered one put back included.
	local file bad=""
	while IFS= read -r file; do
		[ -n "$file" ] || continue
		[ "$(shasum -a 256 "$file" | cut -d' ' -f1)" = "$(basename "$file")" ] || bad="$bad $(basename "$file")"
	done <<EOT
$(find "$(dt1_archives)" -type f ! -name '*.sig' ! -name '*.rmd160' ! -name '*.altered' 2>/dev/null)
EOT
	[ -z "$bad" ] || { row_fail "kept archives that don't match their digests after the row:$bad"; return; }
	[ "${DT1_ALTERED:-0}" -gt 0 ] || { row_fail "no archive of $ACCEPT_LIB_PORT was kept to alter, so the guard wasn't exercised"; return; }
	if grep -qi "isn't the one it was kept as" "$ROW_DIR/out.log"; then
		row_pass "the altered archive was caught and the dependency built instead: $(grep -i "isn't the one it was kept as" "$ROW_DIR/out.log" | head -1)"
	else
		# A harm row: a tamper nothing named is never a known issue.
		row_fail "the second check didn't say it caught the altered archive (exit $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)); whether the guest was given it is in $ROW_DIR/out.log"
	fi
}
