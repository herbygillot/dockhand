# stages: full
# F10: a non-English locale, a clock a few minutes off, and
# serve.outdated_at across the US clock change on 2026-11-01: output and
# the daily look behave; renewal doesn't fail on skew.
act() {
	host_only "the host's locale and clock" || return 0
	LANG=de_DE.UTF-8 LC_ALL=de_DE.UTF-8 "$DH_BIN" status >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	LANG=de_DE.UTF-8 LC_ALL=de_DE.UTF-8 "$DH_BIN" outdated --mine >>"$ROW_DIR/out.log" 2>&1 || echo "[exit $?]" >>"$ROW_DIR/out.log"
	checkpoint "set the clock five minutes off (System Settings), then resume" || return 0
	dh_json auth status || :
	dh_json status --refresh || :
	checkpoint "put the clock right; the clock change is read when the run spans 2026-11-01" || return 0
}
assert() {
	grep -q '^\[exit [1-9]' "$ROW_DIR/out.log" && { row_fail "a command failed in another locale"; return; }
	judged "output behaved in German, and the login renewed despite the skew"
}
