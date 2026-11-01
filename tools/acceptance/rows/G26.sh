# stages: quick full
# G26: names outdated and update can't look up, refused before a look
# (v0.8.0, the outdated-ports session's notes 16 and 18):
#  - an obsolete stub: ansible, at the stage's pinned master, is the
#    obsolete PortGroup's stub, replaced_by py-ansible; outdated lists it
#    under Not checked as "replaced_by py-ansible", where it read as a held
#    major update, and update refuses it, exit 1, naming py-ansible
#    (7aa77308);
#  - an argument holding spaces: outdated "gitui alot" is refused, exit 1,
#    as several port names in one argument, said once, and nothing asked
#    of GitHub (6f8f5145), where it read "no port named …" three times.
act() {
	dh outdated ansible </dev/null || :
	dh_json update ansible --plan </dev/null || :
	dh outdated "gitui alot" </dev/null || :
	dh_json outdated "gitui alot" </dev/null || :
}
assert() {
	local update=$ROW_DIR/json/1.json spaced=$ROW_DIR/json/2.json said
	[ -f "$spaced" ] || { row_fail "a step didn't run"; return; }
	if ! grep -A4 -F '$ dockhand outdated ansible' "$ROW_DIR/out.log" | matches 'ansible .*replaced_by py-ansible'; then
		row_fail "outdated ansible didn't list it as replaced_by py-ansible: $(grep -A4 -F '$ dockhand outdated ansible' "$ROW_DIR/out.log" | sed -n 2,4p | tr '\n' ' ')"
		return
	fi
	if [ "$(cat "$update.exit")" != 1 ]; then
		row_fail "update ansible --plan exited $(cat "$update.exit"), not 1"
		return
	fi
	if ! jq -r '.error // empty' "$update" | matches 'obsolete stub, replaced_by py-ansible.*dockhand update py-ansible'; then
		row_fail "update ansible's refusal didn't name py-ansible: $(jq -r '.error // empty' "$update")"
		return
	fi
	said=$(sed -n '/^\$ dockhand outdated gitui alot$/,/^\[exit/p' "$ROW_DIR/out.log")
	if ! printf '%s\n' "$said" | matches -F '[exit 1]'; then
		row_fail "outdated \"gitui alot\" didn't exit 1: $(printf '%s' "$said" | tail -1)"
		return
	fi
	if [ "$(printf '%s\n' "$said" | grep -c 'several port names in one argument')" != 1 ]; then
		row_fail "outdated \"gitui alot\" didn't say several names in one argument once: $(printf '%s' "$said" | sed 1d | tr '\n' ' ')"
		return
	fi
	if [ "$(jq -r '.github_requests' "$spaced")" != 0 ]; then
		row_fail "outdated \"gitui alot\" asked GitHub $(jq -r '.github_requests' "$spaced") times before refusing"
		return
	fi
	row_pass "ansible listed as replaced_by py-ansible, and update refused: $(jq -r '.error' "$update"); outdated \"gitui alot\" refused once, asking GitHub nothing: $(jq -r '.error' "$spaced")"
}
