# stages: full
# D-I7: a fault in dockhand's own guest handling is said at once, as
# dockhand's, with no second attempt, where it cloned a VM for each of
# three; an error nothing classifies is still tried again (the
# architecture review's X2).
#
# The candidate's own source, built with the acceptance failpoints
# (failpoint_bin), makes the Tart provider's first read of the guest's
# results fail once: tart.results:fault as dockhand's own fault, then, in
# a check of its own, tart.results:error as an error nothing classifies.
# Each check runs here, in the process the failpoint is set for.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

setup() {
	host_only "a check on this Mac's Tart" || return 0
	DI7_FP=$(failpoint_bin) || { row_fail "the candidate couldn't be built with failpoints"; return 1; }
}

act() {
	host_only "a check on this Mac's Tart" || return 0
	[ -n "${DI7_FP:-}" ] || return 0
	dh_setup update "$(port)" --new || return 0
	local branch kind
	branch=$(own_branch)
	[ -n "$branch" ] || { row_fail "the update made no branch"; return 0; }
	dh_setup tidy -b "$branch" -y || return 0
	for kind in fault error; do
		# --fresh: a check that reuses an earlier build starts no guest,
		# and the failpoint is never met (the rc8 full stage, which reused
		# check-57's).
		printf '$ DOCKHAND_FAILPOINT=tart.results:%s dockhand check -b %s --fresh\n' "$kind" "$branch" >>"$ROW_DIR/out.log"
		DOCKHAND_FAILPOINT="tart.results:$kind" "$DI7_FP" check -b "$branch" --fresh </dev/null >"$ROW_DIR/check.$kind" 2>&1
		printf '%s\n' "$?" >"$ROW_DIR/check.$kind.exit"
		cat "$ROW_DIR/check.$kind" >>"$ROW_DIR/out.log"
		printf '[exit %d]\n' "$(cat "$ROW_DIR/check.$kind.exit")" >>"$ROW_DIR/out.log"
	done
}

assert() {
	[ -f "$ROW_DIR/check.fault.exit" ] || return 0
	local kind
	for kind in fault error; do
		grep -q 'runs here' "$ROW_DIR/check.$kind" ||
			{ row_fail "harness: the $kind check didn't run here, so the failpoint wasn't in its process: $(head -2 "$ROW_DIR/check.$kind" | tr '\n' ' ')"; return; }
	done
	local fault=$ROW_DIR/check.fault
	grep -q "the guest's results were read wrong, as a failpoint asked" "$fault" ||
		{ row_fail "the fault check never met the failpoint: $(tail -3 "$fault" | tr '\n' ' ')"; return; }
	grep -q "attempt 2 of" "$fault" &&
		{ row_fail "dockhand's own fault was tried again: $(grep -E 'attempt [0-9] of' "$fault" | tr '\n' ' ')"; return; }
	grep -q "dockhand: .*the fault is dockhand's own, so another attempt would repeat it, and none was made" "$fault" ||
		{ row_fail "dockhand's own fault wasn't said as dockhand's: $(tail -3 "$fault" | tr '\n' ' ')"; return; }
	local error=$ROW_DIR/check.error
	grep -q "the guest's results were lost, as a failpoint asked" "$error" ||
		{ row_fail "the error check never met the failpoint: $(tail -3 "$error" | tr '\n' ' ')"; return; }
	grep -q "attempt 2 of" "$error" ||
		{ row_fail "the unclassified error wasn't tried again: $(grep -E 'attempt [0-9] of' "$error" | tr '\n' ' ')"; return; }
	row_pass "dockhand's own fault was said at once after one attempt; the unclassified error was tried again (exit $(cat "$ROW_DIR/check.error.exit"))"
}
