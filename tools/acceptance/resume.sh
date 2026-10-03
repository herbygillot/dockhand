#!/usr/bin/env bash
# resume.sh answers the full stage's checkpoint: done, once what WAITING
# asked is done or holds; skip, to stop that row as not run; or fail,
# where what it asked a person to judge doesn't hold. With no answer, it
# says what the run waits on.
#
#   tools/acceptance/resume.sh [done|skip|fail]
set -euo pipefail
: "${ACCEPT_STATE:=$HOME/.dockhand-acceptance/full}"
if [ ! -f "$ACCEPT_STATE/waiting" ]; then
	echo "resume.sh: the run in $ACCEPT_STATE waits on nothing" >&2
	exit 1
fi
case "${1:-}" in
"") printf 'WAITING: %s\n' "$(cut -f2- "$ACCEPT_STATE/waiting")" ;;
done | skip | fail) printf '%s\n' "$1" >"$ACCEPT_STATE/resume" ;;
*) echo "resume.sh: answer done, skip, or fail" >&2; exit 2 ;;
esac
