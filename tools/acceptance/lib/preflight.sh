# shellcheck shell=bash
# preflight.sh checks the host and the test account before a run and each
# row, so their own faults are reported as theirs, "blocked: host" or
# "blocked: test account", never as a row's fail (#226). In the v0.8.0
# acceptance C7 failed on a disk-image probe that hung every `tart list`
# after it, and B9 on a test account whose Actions started no run; both
# read as dockhand's until a person traced them.
#
# Each prints what's wrong, one line a fault, and nothing when all's well.
# It needs lib/common.sh's with_timeout and lib/harm.sh's
# fault_images_attached.

# preflight_tart_homes are dockhand's Tart home and Tart's own, each only
# where it's there, since listing makes one.
preflight_tart_homes() {
	local home
	for home in "${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" "${TART_HOME:-$HOME/.tart}"; do
		[ -d "$home" ] && printf '%s\n' "$home"
	done
	return 0
}

# preflight_host is the cheap check each row repeats first: each Tart home
# lists within ACCEPT_TART_WAIT seconds (10 unless set), and no disk image
# under a Tart home is attached while no row runs, as C7's base image was,
# attached read-only by a probe that hung.
preflight_host() {
	local home status pattern images
	if command -v tart >/dev/null 2>&1; then
		while IFS= read -r home; do
			[ -n "$home" ] || continue
			status=0
			(ROW_DIR="" with_timeout "${ACCEPT_TART_WAIT:-10}" env TART_HOME="$home" tart list --format json) >/dev/null 2>&1 || status=$?
			case "$status" in
			0) ;;
			142) printf 'tart list in %s gave no answer within %ss\n' "$home" "${ACCEPT_TART_WAIT:-10}" ;;
			*) printf 'tart list in %s failed (exit %s)\n' "$home" "$status" ;;
			esac
		done <<EOT
$(preflight_tart_homes)
EOT
	fi
	pattern=$(preflight_tart_homes | sed -e 's/[][\.*^$()+?{}|]/\\&/g' | paste -sd '|' -)
	if [ -n "$pattern" ] && command -v hdiutil >/dev/null 2>&1; then
		images=$(fault_images_attached "$pattern" | tr '\n' ' ')
		[ -z "$images" ] || printf 'a disk image under a Tart home is still attached (%s): detach it with hdiutil detach\n' "${images% }"
	fi
	return 0
}

# preflight_account is the full stage's check, once at its start, that the
# test account's fork starts an Actions run for a branch pushed to it: a
# branch of the run's own, at the fork's master, which MacPorts' workflow
# builds nothing for, removed after with its run canceled. It waits up to
# ACCEPT_ACTIONS_WAIT seconds (180 unless set). It needs gh, the fork as
# ACCEPT_SANDBOX or DOCKHAND_PULL_REQUESTS, and MACPORTS_TREE's origin
# as that fork, as reset-user.sh clones it.
preflight_account() {
	local fork=${ACCEPT_SANDBOX:-${DOCKHAND_PULL_REQUESTS:-}} branch tree=${MACPORTS_TREE:-} deadline run=""
	[ -n "$fork" ] && [ -n "$tree" ] && command -v gh >/dev/null 2>&1 || return 0
	branch="accept-preflight-$(date -u +%Y%m%d%H%M%S)-$$"
	if ! git -C "$tree" push -q origin "refs/remotes/origin/master:refs/heads/$branch" 2>/dev/null; then
		printf 'a branch could not be pushed to the fork %s\n' "$fork"
		return 0
	fi
	deadline=$((SECONDS + ${ACCEPT_ACTIONS_WAIT:-180}))
	while [ "$SECONDS" -lt "$deadline" ]; do
		run=$(gh api "repos/$fork/actions/runs?branch=$branch&event=push" --jq '.workflow_runs[0].id // empty' 2>/dev/null || :)
		[ -z "$run" ] || break
		sleep 10
	done
	[ -z "$run" ] || gh api -X POST "repos/$fork/actions/runs/$run/cancel" >/dev/null 2>&1 || :
	git -C "$tree" push -q origin --delete "$branch" 2>/dev/null || :
	[ -n "$run" ] || printf "the fork %s's Actions started no run for a branch pushed to it, within %ss\n" "$fork" "${ACCEPT_ACTIONS_WAIT:-180}"
	return 0
}
