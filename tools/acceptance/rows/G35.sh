# stages: quick full
# G35: an update chooses a GitHub port's source (v0.8.0). mindforger, as
# at the quick stage's pin, is 2.0.0, github.tarball_from tarball and
# fetched with Git for its submodules. By default its update to 2.5.0
# leaves it on its clone and says, a notice, that the release's own
# mindforger_2.5.0_tarball.tgz carries the source and deps/cmark-gfm. With
# --source releases it moves there, as #35195 moved it, less the comment
# it wrote: github.tarball_from releases, distname, extract.suffix,
# worksrcdir, and the checksums #35195 wrote, with fetch.type git and the
# submodule hook gone, said among what it fixed. Again with --source keep,
# the clone stays and nothing moves.
# Each branch first sets mindforger's directory back to the pin's, so a
# day's master, which has 2.6.0, still has it to update.
g35_pin() { printf '%s' "${ACCEPT_PIN:-026b3878a258b85619633709d89871f2f8750b94}"; }
g35_dir() { printf 'editors/mindforger'; }

# g35_branch starts a branch of mindforger, its directory as at the pin.
g35_branch() {
	local name=$1 worktree
	dh_setup start "$name" --port mindforger || return 1
	worktree=$("$DH_BIN" path "$name") || { row_fail "dockhand path $name found no worktree"; return 1; }
	git -C "$worktree" checkout "$(g35_pin)" -- "$(g35_dir)" || { row_fail "$(g35_dir) isn't at the pin, $(g35_pin), in $worktree"; return 1; }
	git -C "$worktree" diff --cached --quiet && return 0
	git -C "$worktree" -c user.name=acceptance -c user.email=acceptance@example.invalid -c commit.gpgsign=false \
		commit -q -m "mindforger: as at the acceptance pin" || { row_fail "couldn't commit $(g35_dir) as at the pin"; return 1; }
}

act() {
	G35_DEFAULT=$(run_name g35-default)
	G35_MOVED=$(run_name g35-moved)
	G35_KEPT=$(run_name g35-kept)
	g35_branch "$G35_DEFAULT" || return 0
	dh_json update mindforger 2.5.0 -b "$G35_DEFAULT" || return 0
	g35_branch "$G35_MOVED" || return 0
	dh_json update mindforger 2.5.0 -b "$G35_MOVED" --source releases || return 0
	g35_branch "$G35_KEPT" || return 0
	dh_json update mindforger 2.5.0 -b "$G35_KEPT" --source keep || :
}

# g35_portfile is a branch's mindforger Portfile, as its worktree has it.
g35_portfile() {
	local dir
	dir=$("$DH_BIN" path "$1" 2>/dev/null) || return 1
	cat "$dir/$(g35_dir)/Portfile" 2>/dev/null
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	local n file portfile line
	for n in 1 2 3; do
		file=$ROW_DIR/json/$n.json
		[ "$(cat "$file.exit" 2>/dev/null)" = 0 ] || { row_fail "dockhand $(cat "$file.args" 2>/dev/null) exited $(cat "$file.exit" 2>/dev/null): $(jq -r '.error // empty' "$file" 2>/dev/null)"; return; }
	done
	portfile=$(g35_portfile "$G35_DEFAULT") || { row_fail "no Portfile of mindforger on $G35_DEFAULT"; return; }
	printf '%s\n' "$portfile" | matches -E '^fetch\.type[[:space:]]+git$' && printf '%s\n' "$portfile" | matches -E '^github\.tarball_from[[:space:]]+tarball$' ||
		{ row_fail "the default update moved mindforger's source: $(printf '%s\n' "$portfile" | grep -E '^(fetch|github\.tarball)' | tr '\n' ';')"; return; }
	jq -e '[.. | objects | select(.rule? == "source" and .hold == false and (.message | startswith("release 2.5.0'"'"'s mindforger_2.5.0_tarball.tgz carries the source") and contains("deps/cmark-gfm") and endswith("; update --source releases fetches it")))] | length == 1' "$ROW_DIR/json/1.json" >/dev/null ||
		{ row_fail "the default update didn't say, a notice, that the release's tarball carries the source: $(jq -c '[.. | objects | select(.rule? == "source")]' "$ROW_DIR/json/1.json")"; return; }
	jq -e '[.. | .fixed? // empty | .[] | select(startswith("fetch.type git is now"))] | length == 0' "$ROW_DIR/json/1.json" >/dev/null ||
		{ row_fail "the default update said a move: $(jq -c '[.. | .fixed? // empty]' "$ROW_DIR/json/1.json")"; return; }
	jq -e '[.. | .fixed? // empty | .[] | select(startswith("fetch.type git is now release 2.5.0'"'"'s mindforger_2.5.0_tarball.tgz, which carries its Git submodules") and contains("deps/cmark-gfm"))] | length == 1' "$ROW_DIR/json/2.json" >/dev/null ||
		{ row_fail "--source releases didn't say the move among what it fixed: $(jq -c '[.. | .fixed? // empty]' "$ROW_DIR/json/2.json")"; return; }
	portfile=$(g35_portfile "$G35_MOVED") || { row_fail "no Portfile of mindforger on $G35_MOVED"; return; }
	for line in '^github\.tarball_from[[:space:]]+releases$' '^distname[[:space:]]+\$\{name\}_\$\{version\}_tarball$' '^extract\.suffix[[:space:]]+\.tgz$' \
		'^worksrcdir[[:space:]]+ci_\$\{name\}_\$\{version\}$' '^checksums[[:space:]]+rmd160[[:space:]]+64ce69042956182693da5282feebd4c42c6fc18b' 'size[[:space:]]+21845391$'; do
		printf '%s\n' "$portfile" | matches -E "$line" || { row_fail "mindforger's Portfile on $G35_MOVED lacks $line"; return; }
	done
	if printf '%s\n' "$portfile" | matches -E '^fetch\.type|git submodule update|^# Change github\.tarball_from|release tarball carries'; then
		row_fail "mindforger's Portfile on $G35_MOVED keeps its clone, the stock comment, or wrote one: $(printf '%s\n' "$portfile" | grep -E '^fetch|submodule|^#' | tr '\n' ';')"
		return
	fi
	portfile=$(g35_portfile "$G35_KEPT") || { row_fail "no Portfile of mindforger on $G35_KEPT"; return; }
	printf '%s\n' "$portfile" | matches -E '^fetch\.type[[:space:]]+git$' && printf '%s\n' "$portfile" | matches -E '^github\.tarball_from[[:space:]]+tarball$' ||
		{ row_fail "--source keep moved mindforger's source: $(printf '%s\n' "$portfile" | grep -E '^(fetch|github\.tarball)' | tr '\n' ';')"; return; }
	row_pass "mindforger's update to 2.5.0 left its clone and said the release's tarball carries the source; --source releases moved it there as #35195 did, less the comment, and said so; --source keep left its clone"
}
