# stages: quick full
# B2: one shot, with no serve running: bump on the small Rust port, asking
# nothing, to its pull request, or exiting 3 where it's held, naming the
# command that finishes it. bump settles what would stop its submit before
# it edits anything, so in a quick stage without ACCEPT_GH_FORK it refuses
# at once, naming the fork.
#
# With ACCEPT_GH_FORK it goes through a whole Tart check, and the guest
# builds the port's toolchain first: MacPorts has no binary archives of
# rust or cargo for a macOS release before its own builders do, and the
# base image has none, so on macOS 27 that is hours on two vCPUs (the M1's
# run at d302e744). The quick stage doesn't run it then, unless
# ACCEPT_B2_BUILD=1 asks for the build; the full stage runs it through.
# Where it builds, a second check of the branch, --fresh, must install
# rust and cargo from the archives the first kept (batch 90).
act() {
	if [ "${ACCEPT_STAGE:-}" = quick ] && [ -n "${ACCEPT_GH_FORK:-}" ] && [ "${ACCEPT_B2_BUILD:-0}" != 1 ]; then
		row_result "not run" "with a fork, bump runs a whole check, whose guest builds rust and cargo from source where MacPorts has no archives for its release; ACCEPT_B2_BUILD=1 runs it"
		return 0
	fi
	dh_json bump "${ACCEPT_RUST_PORT:?}" </dev/null || :
	b2_reuse
}

# b2_reuse checks the bump's branch again, --fresh, in the same home: the
# port builds again, and its guest is given the archives the first guest
# installed its toolchain from, which batch 90 keeps, so rust and cargo
# aren't built a second time (the M1's run at 464d583c: the stage's reset
# deleted them between B2 and B3, so B3's guest built them from zero). The
# second check's log of the port says which.
b2_reuse() {
	local branch
	branch=$(git -C "${MACPORTS_TREE:?}" for-each-ref --format='%(refname:short)' 'refs/heads/dockhand/*' | head -1)
	[ -n "$branch" ] || return 0
	branch=${branch#dockhand/}
	dh_json check -b "$branch" --fresh </dev/null || :
	B2_REUSE=$DH_LAST_JSON
	local check
	check=$(jq -r '.result.run.name // empty' "$B2_REUSE")
	[ -n "$check" ] && dh logs "$check" --port "$ACCEPT_RUST_PORT" >"$ROW_DIR/reuse.log" 2>&1 || :
}

assert() {
	local file=$ROW_DIR/json/1.json status error
	[ -f "$file.exit" ] || return 0
	status=$(cat "$file.exit")
	error=$(jq -r '.error // ""' "$file")
	if grep -qE '\[y/N\]|\[Y/n\]' "$ROW_DIR/out.log"; then
		row_fail "bump asked something"
		return
	fi
	# The second check's guest installs the toolchain the first built.
	if [ -n "${B2_REUSE:-}" ]; then
		if [ "$(cat "$B2_REUSE.exit")" != 0 ]; then
			row_fail "the second check, --fresh, exited $(cat "$B2_REUSE.exit"): $(jq -r '.error // ""' "$B2_REUSE")"
			return
		fi
		if grep -qE '^--->  Building (rust|cargo) ' "$ROW_DIR/reuse.log"; then
			row_fail "the second check's guest built $(grep -oE '^--->  Building (rust|cargo) ' "$ROW_DIR/reuse.log" | awk '{print $3}' | sort -u | paste -sd ' ' -) again, rather than install the archives the first kept"
			return
		fi
		if ! grep -q 'from archives earlier checks' "$ROW_DIR/out.log"; then
			row_fail "the second check gave its guest no kept dependency archive"
			return
		fi
	fi
	case "$status" in
	0) row_pass "bumped to its pull request" ;;
	3) if grep -q '^Once it.s fine: dockhand submit' "$ROW_DIR/out.log" || printf '%s' "$error" | grep -q 'dockhand submit'; then
		row_pass "held for a look, naming the submit that finishes it"
	else
		row_fail "held without naming the command that finishes it: $error"
	fi ;;
	*) if printf '%s' "$error" | grep -qi fork && [ -z "$(git -C "$MACPORTS_TREE" branch --list 'dockhand/*')" ]; then
		row_refused_well "refused before editing anything, for want of a GitHub fork, which the quick stage hasn't: $error"
	else
		row_fail "bump exited $status: $error"
	fi ;;
	esac
}
