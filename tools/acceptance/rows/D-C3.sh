# stages: full
# D-C3: dockhand's authorization revoked on GitHub: the next command
# needing it says the login was revoked, not a raw 401 (H4 holds).
act() {
	host_only "a GitHub authorization to revoke" || return 0
	checkpoint "revoke dockhand's authorization on the test account (GitHub, Settings, Applications)" || return 0
	dh_json status --refresh || :
	# A revoked login is dead, so the rows after need a new one; it's
	# asked for here, beside the revocation, where the person already is.
	device_login "D-C3 logs dockhand in again after the revocation" || :
}
assert() {
	jq -r '.error // empty' "$ROW_DIR/json/1.json" | grep -qi 'revoked' && ! grep -q '401' "$ROW_DIR/out.log" &&
		row_pass "said the login was revoked" || row_fail "not said as revoked: $(jq -r '.error // empty' "$ROW_DIR/json/1.json")"
}
