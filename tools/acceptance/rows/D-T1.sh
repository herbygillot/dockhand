# stages: quick full
# D-T1: a kept archive altered by one byte. A check that would install it
# catches it by its digest, and builds the port rather than install it,
# saying so.
. "${ROW_LIB:?}/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
act() {
	dh update "$(port)" --new || return 0
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
	if grep -qiE 'digest|doesn.t match|altered|corrupt' "$ROW_DIR/out.log"; then
		row_pass "the altered archive was caught: $(grep -iE 'digest|doesn.t match|altered|corrupt' "$ROW_DIR/out.log" | head -1)"
	elif [ "$(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null)" = 0 ]; then
		row_known "the check passed without saying it caught the altered archive; whether it installed it is in $ROW_DIR/out.log"
	else
		row_fail "the second check exited $(cat "$ROW_DIR/json/1.json.exit" 2>/dev/null) without naming the archive"
	fi
}
