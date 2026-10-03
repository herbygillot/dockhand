# stages: quick full
# D-T1: a kept archive altered by one byte. A check that would install it
# catches it by its digest, and builds the port rather than install it,
# saying so ("the archive kept of X isn't the one it was kept as").
. "${ROW_LIB:?}/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	# A kept archive is given to a guest only as a dependency of what it
	# builds, and the quick stage's ports depend on none: its check of the
	# port itself builds the port, and never reads the altered file. The
	# engine's TestTheGuestInstallsWhatABuildNeedsFromItsKeptArchive
	# alters one a guest would be given, which it builds instead, saying
	# so; the full stage's run gives a guest one.
	if [ "${ACCEPT_STAGE:-}" = quick ]; then
		row_result "not run" "the stage's ports depend on no kept archive, so none is given to a guest; the engine's test covers an altered one"
		return 0
	fi
	dh_setup update "$(port)" --new || return 0
	dh check -p "$(port)" || return 0
	local archives archive
	archives="$(dirname "${DOCKHAND_DB:?}")/archives"
	archive=$(find "$archives" -type f ! -name '*.sig' ! -name '*.rmd160' | head -1)
	[ -n "$archive" ] || return 0
	fault_flip "$archive"
	DT1_ARCHIVE=$archive
	dh_json check -p "$(port)" --fresh || :
}
assert() {
	[ -n "${DT1_ARCHIVE:-}" ] || { row_known "no archive was kept to alter"; return; }
	if grep -qiE 'isn.t the one it was kept as|digest|doesn.t match|altered|corrupt' "$ROW_DIR/out.log"; then
		row_pass "the altered archive was caught: $(grep -iE 'digest|doesn.t match|altered|corrupt' "$ROW_DIR/out.log" | head -1)"
	elif [ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ]; then
		row_known "the check passed without saying it caught the altered archive; whether it installed it is in $ROW_DIR/out.log"
	else
		row_fail "the second check exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null) without naming the archive"
	fi
}
