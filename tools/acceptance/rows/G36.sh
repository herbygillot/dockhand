# stages: full
# G36: an update chooses a GitHub port's source on a fixture of the test
# account's (v0.8.0), and the moved port builds. host/make-source-fixture.sh
# made ACCEPT_SOURCE_FIXTURE's project, fetched with Git for its submodule
# deps/sub, whose v1.1.0 release carries <name>_1.1.0_tarball.tgz with the
# submodule's files and <name>-1.1.0.tar.gz without them, and its port's
# Portfile, at 1.0.0, in the host's fixtures directory, which each branch
# takes as devel/<name>/Portfile. By default the update to 1.1.0 leaves
# the clone and says the .tgz carries the source and deps/sub; with
# --source releases it moves there, the plain archive passed over as
# lacking deps/sub; and a Tart check of the moved port passes, its
# Makefile's install needing the submodule's file. Unset, the row isn't
# run.
g36_name() { printf '%s' "${ACCEPT_SOURCE_FIXTURE#*/}"; }
g36_portfile() { printf '%s/fixtures/%s/Portfile' "${ACCEPT_HOST_ROOT:-/Users/Shared/dockhand-acceptance}" "$(g36_name)"; }

# g36_branch starts a branch and gives it the fixture's port, committed.
g36_branch() {
	local name=$1 worktree dir
	dh_setup start "$name" || return 1
	worktree=$("$DH_BIN" path "$name") || { row_fail "dockhand path $name found no worktree"; return 1; }
	dir="$worktree/devel/$(g36_name)"
	mkdir -p "$dir" && cp "$(g36_portfile)" "$dir/Portfile" || { row_fail "couldn't put $(g36_portfile) in $dir"; return 1; }
	git -C "$worktree" add --sparse "devel/$(g36_name)/Portfile" &&
		git -C "$worktree" -c user.name=acceptance -c user.email=acceptance@example.invalid -c commit.gpgsign=false \
			commit -q -m "$(g36_name): new port, 1.0.0" || { row_fail "couldn't commit the fixture's port on $name"; return 1; }
}

act() {
	host_only "a Tart check of a moved port" || return 0
	if [ -z "${ACCEPT_SOURCE_FIXTURE:-}" ] || [ ! -f "$(g36_portfile)" ]; then
		row_result "not run" "run tools/acceptance/host/make-source-fixture.sh --run as the test account, then set ACCEPT_SOURCE_FIXTURE to the project it made"
		return 0
	fi
	G36_DEFAULT=$(run_name g36-default)
	G36_MOVED=$(run_name g36-moved)
	g36_branch "$G36_DEFAULT" || return 0
	dh_json update "$(g36_name)" 1.1.0 -b "$G36_DEFAULT" || return 0
	g36_branch "$G36_MOVED" || return 0
	dh_json update "$(g36_name)" 1.1.0 -b "$G36_MOVED" --source releases || return 0
	dh_json check -b "$G36_MOVED" --on tart || :
}

g36_text() {
	local dir
	dir=$("$DH_BIN" path "$1" 2>/dev/null) || return 1
	cat "$dir/devel/$(g36_name)/Portfile" 2>/dev/null
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	local n file portfile name
	name=$(g36_name)
	for n in 1 2; do
		file=$ROW_DIR/json/$n.json
		[ "$(cat "$file.exit" 2>/dev/null)" = 0 ] || { row_fail "dockhand $(cat "$file.args" 2>/dev/null) exited $(cat "$file.exit" 2>/dev/null): $(jq -r '.error // empty' "$file" 2>/dev/null)"; return; }
	done
	portfile=$(g36_text "$G36_DEFAULT") || { row_fail "no Portfile of $name on $G36_DEFAULT"; return; }
	printf '%s\n' "$portfile" | matches -E '^fetch\.type[[:space:]]+git$' ||
		{ row_fail "the default update moved $name's source: $(printf '%s\n' "$portfile" | grep -E '^(fetch|github\.tarball)' | tr '\n' ';')"; return; }
	jq -e --arg asset "${name}_1.1.0_tarball.tgz" '[.. | objects | select(.rule? == "source" and .hold == false and (.message | contains($asset) and contains("deps/sub") and endswith("; update --source releases, run instead of this update, fetches it")))] | length == 1' "$ROW_DIR/json/1.json" >/dev/null ||
		{ row_fail "the default update didn't say, a notice, that the release's .tgz carries the source: $(jq -c '[.. | objects | select(.rule? == "source")]' "$ROW_DIR/json/1.json")"; return; }
	portfile=$(g36_text "$G36_MOVED") || { row_fail "no Portfile of $name on $G36_MOVED"; return; }
	printf '%s\n' "$portfile" | matches -E '^github\.tarball_from[[:space:]]+releases$' || { row_fail "--source releases didn't move $name to its release: $(printf '%s\n' "$portfile" | grep -E '^(fetch|github|distname|extract)' | tr '\n' ';')"; return; }
	if printf '%s\n' "$portfile" | matches -E '^fetch\.type|git submodule update'; then
		row_fail "$name's Portfile on $G36_MOVED keeps its clone: $(printf '%s\n' "$portfile" | grep -E '^fetch|submodule' | tr '\n' ';')"
		return
	fi
	jq -e --arg asset "${name}_1.1.0_tarball.tgz" '[.. | .fixed? // empty | .[] | select(startswith("fetch.type git is now release") and contains($asset))] | length == 1' "$ROW_DIR/json/2.json" >/dev/null ||
		{ row_fail "--source releases didn't say the move among what it fixed: $(jq -c '[.. | .fixed? // empty]' "$ROW_DIR/json/2.json")"; return; }
	file=$ROW_DIR/json/3.json
	[ "$(cat "$file.exit" 2>/dev/null)" = 0 ] || { row_fail "the Tart check of the moved $name exited $(cat "$file.exit" 2>/dev/null): $(jq -r '.error // empty' "$file" 2>/dev/null)"; return; }
	row_pass "$name's update to 1.1.0 left its clone and said its release's .tgz carries the source and deps/sub; --source releases moved it there, and the Tart check of the moved port passed"
}
