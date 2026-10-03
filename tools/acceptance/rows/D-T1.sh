# stages: quick full
# D-T1: a kept archive altered by one byte. A check that would install it
# catches it by its digest, and builds the port rather than install it,
# saying so ("the archive kept of X isn't the one it was kept as").
#
# A kept archive is given to a guest only as a dependency of what it
# builds, reused from an earlier check. So the row revbumps a dependency,
# ACCEPT_LIB_PORT, and a port that depends on it, ACCEPT_LIB_DEPENDENT, in
# one branch, checks both, which keeps the dependency's archive, alters
# every kept archive by a byte, edits only the dependent, and checks
# again: the dependency's result is reused, its archive would be given to
# the guest, and the digest must stop it. The quick stage's ports depend on
# none, and it doesn't run this; the engine's
# TestTheGuestInstallsWhatABuildNeedsFromItsKeptArchive alters one there.
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
	dh_setup revbump "$ACCEPT_LIB_PORT" "$ACCEPT_LIB_DEPENDENT" --branch dt1-tamper --subject "rebuild to test a tampered archive" || return 0
	dh_setup check -b dt1-tamper || return 0
	local archives archive dir
	archives="$(dirname "${DOCKHAND_DB:?}")/archives"
	DT1_ALTERED=0
	while IFS= read -r archive; do
		[ -n "$archive" ] || continue
		fault_flip "$archive"
		DT1_ALTERED=$((DT1_ALTERED + 1))
	done <<EOT
$(find "$archives" -type f ! -name '*.sig' ! -name '*.rmd160' 2>/dev/null)
EOT
	dir=$("$DH_BIN" path dt1-tamper) || return 0
	printf '\n# rebuilt with its dependency reused, to test a tampered archive\n' >>"$dir/$(dh_quiet --json status dt1-tamper | jq -r --arg p "$ACCEPT_LIB_DEPENDENT" '.result.branches[0].directories[] | select(endswith("/" + $p))')/Portfile"
	dh_json check -b dt1-tamper || :
	# The check's events, as its record keeps them, read from the stage's
	# own database, opened read-only.
	sqlite3 "file:${DOCKHAND_DB}?mode=ro" "SELECT message FROM events WHERE kind = 'archive.altered'" >>"$ROW_DIR/out.log" 2>&1 || :
}
assert() {
	[ "${DT1_ALTERED:-0}" -gt 0 ] || { row_fail "no archive was kept to alter, so the guard wasn't exercised"; return; }
	if grep -qi "isn't the one it was kept as" "$ROW_DIR/out.log"; then
		row_pass "the altered archive was caught and the dependency built instead: $(grep -i "isn't the one it was kept as" "$ROW_DIR/out.log" | head -1)"
	else
		# A harm row: a tamper nothing named is never a known issue.
		row_fail "the second check didn't say it caught the altered archive (exit $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)); whether the guest was given it is in $ROW_DIR/out.log"
	fi
}
