# stages: full
# C1: login renewal. B7 covers it overnight; here auth status before and
# after the expiry says until when it renews, and never asks for a login
# it can renew.
act() {
	host_only "a GitHub login near its expiry" || return 0
	dh auth status || :
	checkpoint "wait past the token's 8-hour expiry, with no serve running" || return 0
	dh auth status || :
	dh_json status --refresh || :
}
assert() {
	grep -qi 'auth login' <(sed -n '/^\$ dockhand --json status --refresh/,$p' "$ROW_DIR/out.log") &&
		{ row_fail "it asked for a login it could renew"; return; }
	judged "auth status said until when it renews, before and after"
}
