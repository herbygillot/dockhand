# stages: full
# C7: clean over merged, closed, and archived branches, one with edits,
# and one whose fork branch moved after merge: each kept or removed as
# usage.md says, H1 and H2 holding. The test PRs' clean --closed gives
# the closed ones.
act() {
	host_only "merged and closed pull requests" || return 0
	checkpoint "make sure this run has a merged, a closed, and an archived branch, one with an uncommitted edit, and one whose fork branch moved after merge" || return 0
	dh_json clean --closed || :
	allow_change "*"
	allow_ref_gone "*"
	dh_json clean --closed -y || :
	dh status --all || :
}
assert() {
	[ "$(cat "$ROW_DIR/json/2.json.exit" 2>/dev/null)" = 0 ] || { row_fail "clean failed"; return; }
	judged "clean kept the branch with edits and the moved fork branch, and removed the rest as usage.md says"
}
