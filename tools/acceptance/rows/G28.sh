# stages: quick full
# G28: a renamed port is compared with its predecessor (1f2e8553):
#  - renamed with dockhand rename, gitui to gitui2, with its obsolete stub
#    left at devel/gitui, and tidied: submit --plan's assessment compares
#    gitui2 with gitui, so it finds nothing upstream to say, where a new
#    port's Cargo.lock reads as adding each crate, as a copy of gitui under
#    a new name with no stub does;
#  - renamed in its own directory, ap-utils' name line made ap-utils2 with
#    its version 1.4.1: tidy's subject is "ap-utils2: rename from
#    ap-utils, update to 1.4.1", where it was "ap-utils: update to 1.4.1".
# The rename's text names check, which the quick stage run without an
# image can't plan, so it's read as JSON. In the full stage it works on a
# master of its own at the quick stage's pin (plan_at_pin): MacPorts' own
# master has ap-utils at 1.4.1 already (rc4's full stage).
. "${ROW_LIB:?}/plan.sh"
setup() {
	plan_at_pin "ap-utils is at 1.3.3" || return 1
	G28_RENAMED=$(run_name g28-rename)
	G28_NEW=$(run_name g28-new)
	G28_INPLACE=$(run_name g28-inplace)
}
act() {
	local dir
	dh_json rename gitui gitui2 --new="$G28_RENAMED" </dev/null || return 0
	dh_json tidy -b "$G28_RENAMED" -y </dev/null || return 0
	dh_json submit -b "$G28_RENAMED" --plan </dev/null || :
	# The same Portfile as a new port, for the contrast.
	dh_setup start "$G28_NEW" --port gitui </dev/null || return 0
	dir=$("$DH_BIN" path "$G28_NEW") || return 0
	mkdir -p "$dir/devel/gitui3"
	sed 's/^\(github\.setup  *extrawurst gitui .*\)$/\1\
name                gitui3/' "$dir/devel/gitui/Portfile" >"$dir/devel/gitui3/Portfile"
	allow_change "$dir/*"
	git -C "$dir" add --sparse devel/gitui3 >>"$ROW_DIR/out.log" 2>&1 &&
		git -C "$dir" commit -q -m "gitui3: new port" >>"$ROW_DIR/out.log" 2>&1 || return 0
	dh_json submit -b "$G28_NEW" --plan </dev/null || :
	dh_setup start "$G28_INPLACE" --port ap-utils </dev/null || return 0
	dir=$("$DH_BIN" path "$G28_INPLACE") || return 0
	perl -pi -e 's/^(name\s+)ap-utils$/${1}ap-utils2/; s/^(version\s+)1\.3\.3$/${1}1.4.1/' "$dir/net/ap-utils/Portfile"
	allow_change "$dir/*"
	dh_json tidy -b "$G28_INPLACE" --plan </dev/null || :
}
assert() {
	local renamed=$ROW_DIR/json/3.json new=$ROW_DIR/json/4.json inplace=$ROW_DIR/json/5.json
	[ -f "$renamed" ] || { row_fail "the rename's submit --plan didn't run: $(jq -r '.error // empty' "$ROW_DIR"/json/[12].json 2>/dev/null | tr '\n' ' ')"; return; }
	if [ "$(jq -r '.result.title // empty' "$renamed")" != "gitui2: rename from gitui" ]; then
		row_fail "the rename's title is $(jq -r '.result.title // "none"' "$renamed"), not gitui2: rename from gitui"
		return
	fi
	if [ "$(jq '.result.upstream | length' "$renamed")" != 0 ]; then
		row_fail "the renamed gitui2 was assessed as a new port, not with gitui: $(jq -r '.result.upstream[]? | tostring' "$renamed" | head -2 | tr '\n' ' ')"
		return
	fi
	[ -f "$new" ] || { row_fail "the new port's submit --plan didn't run"; return; }
	if ! jq -r '.result.upstream[]? | tostring' "$new" | matches 'Cargo.lock adds'; then
		row_fail "gitui3, a new port, wasn't read as adding its crates, so the rename's quiet shows nothing: $(jq -c '.result.upstream' "$new" | cut -c1-300)"
		return
	fi
	[ -f "$inplace" ] || { row_fail "tidy --plan of the rename in its own directory didn't run"; return; }
	if ! jq -r '.. | strings' "$inplace" | matches -xF 'ap-utils2: rename from ap-utils, update to 1.4.1'; then
		row_fail "tidy's subject for ap-utils renamed in its directory isn't ap-utils2: rename from ap-utils, update to 1.4.1: $(jq -r '.. | strings | select(test("ap-utils"))' "$inplace" | sort -u | head -3 | tr '\n' ';')"
		return
	fi
	row_pass "gitui2, renamed, compared with gitui and nothing said, where gitui3, new, read as adding its crates; ap-utils renamed in its directory tidied as ap-utils2: rename from ap-utils, update to 1.4.1"
}
