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
act() {
	if [ "${ACCEPT_STAGE:-}" = quick ] && [ -n "${ACCEPT_GH_FORK:-}" ] && [ "${ACCEPT_B2_BUILD:-0}" != 1 ]; then
		row_result "not run" "with a fork, bump runs a whole check, whose guest builds rust and cargo from source where MacPorts has no archives for its release; ACCEPT_B2_BUILD=1 runs it"
		return 0
	fi
	dh_json bump "${ACCEPT_RUST_PORT:?}" </dev/null || :
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
