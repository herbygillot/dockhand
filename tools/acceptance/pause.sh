#!/usr/bin/env bash
# pause.sh asks a running stage to stop at its next row boundary, as when
# a new candidate is about to be installed mid-run (#225: rc4's C2
# straddled two candidates). The runner finishes the row it's in, writes
# the rows left to results/<candidate>/remaining.txt, and stops; run them
# with --rows-file once it's settled.
#
#   tools/acceptance/pause.sh
set -euo pipefail
: "${ACCEPT_STATE:=$HOME/.dockhand-acceptance/full}"
[ -d "$ACCEPT_STATE" ] || { echo "pause.sh: no stage at $ACCEPT_STATE" >&2; exit 1; }
: >"$ACCEPT_STATE/pause"
echo "pause.sh: the run stops before its next row"
