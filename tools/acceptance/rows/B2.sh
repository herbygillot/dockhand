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
# ACCEPT_B2_BUILD=1 asks for the build; the full stage runs it through,
# but for a dry run.
# Where it builds, a second check of the branch, --fresh, must install
# rust and cargo from the archives the first kept (batch 90).
# prs: ${ACCEPT_RUST_PORT} test
act() {
	# A dry run of the full stage runs in the quick stage's environment,
	# fork and all, and built rust and cargo too (the M1's dry run at
	# 90de4fc2): only a live full stage, or ACCEPT_B2_BUILD=1, builds.
	if [ -n "${ACCEPT_GH_FORK:-}" ] && [ "${ACCEPT_B2_BUILD:-0}" != 1 ] && { [ "${ACCEPT_STAGE:-}" = quick ] || [ "${ACCEPT_DRY:-0}" = 1 ]; }; then
		row_result "not run" "with a fork, bump runs a whole check, whose guest builds rust and cargo from source where MacPorts has no archives for its release; ACCEPT_B2_BUILD=1 runs it"
		return 0
	fi
	# bump goes on to its pull request, in the sandbox the stage names:
	# the one H3 and H2 allow, and the approval list names (the rc6 full
	# stage found neither).
	allow_prs 1
	allow_push "*dockhand/${ACCEPT_RUST_PORT}*"
	# A test pull request says it is one, as submit_pr's do.
	local test=()
	if [ "$(pr_kind "$ACCEPT_RUST_PORT")" != real ]; then
		test=(--title "[testing] ${ACCEPT_RUST_PORT}: dockhand ${ACCEPT_CANDIDATE:-} bump" --skip-notification
			--note "This pull request tests a dockhand release candidate, ${ACCEPT_CANDIDATE:-}, and will be closed without merging.")
	fi
	dh_json bump "${ACCEPT_RUST_PORT:?}" "${test[@]}" </dev/null || :
	b2_reuse
	# Its test pull request closes once the row has its evidence: left
	# open, its branch stayed, and B5 and D-I4 saw two of the port's (the
	# rc6 full stage).
	close_test_pr "$ACCEPT_RUST_PORT" "$(own_branch)"
}

# b2_reuse checks the bump's branch again, --fresh, in the same home: the
# port builds again, and its guest is given the archives the first guest
# installed its toolchain from, which batch 90 keeps, so rust and cargo
# aren't built a second time (the M1's run at 464d583c: the stage's reset
# deleted them between B2 and B3, so B3's guest built them from zero). The
# second check's log of the port says which.
b2_reuse() {
	local branch
	# The bump's own branch, not the first of any left open: that was
	# A12's (the rc6 full stage).
	branch=$(own_branch)
	[ -n "$branch" ] || return 0
	dh_json check -b "$branch" --fresh </dev/null || :
	B2_REUSE=$DH_LAST_JSON
	local check
	check=$(jq -r '.result.run.name // empty' "$B2_REUSE")
	# Straight to its own file: dh writes to the row's log (the M1's run
	# at 11491d4e found reuse.log empty).
	if [ -n "$check" ]; then
		printf '$ dockhand logs %s --port %s >reuse.log\n' "$check" "$ACCEPT_RUST_PORT" >>"$ROW_DIR/out.log"
		"$DH_BIN" logs "$check" --port "$ACCEPT_RUST_PORT" >"$ROW_DIR/reuse.log" 2>&1 || :
	fi
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
		if [ ! -s "$ROW_DIR/reuse.log" ]; then
			row_fail "the second check's log of $ACCEPT_RUST_PORT couldn't be read, so whether its guest built rust again isn't known"
			return
		fi
		if ! grep -q 'from the archive an earlier guest installed it from' "$ROW_DIR/out.log"; then
			row_fail "the second check gave its guest no kept dependency archive"
			return
		fi
		B2_REUSED="its second check, --fresh, installed rust and cargo from the archives the first kept ($(grep -c 'from the archive an earlier guest installed it from' "$ROW_DIR/out.log") given)"
	fi
	# With a fork, bump goes through its check and stops at the push,
	# which the quick stage refuses (lib/ssh-read-only.sh): that is the
	# stage, and the row is graded on the rest.
	if [ "$status" != 0 ] && [ "${ACCEPT_STAGE:-}" = quick ] && grep -q 'the quick stage never pushes' "$ROW_DIR/out.log"; then
		row_pass "bump checked and stopped at the stage's refused push${B2_REUSED:+; $B2_REUSED}"
		return
	fi
	case "$status" in
	0) row_pass "bumped to its pull request${B2_REUSED:+; $B2_REUSED}" ;;
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
