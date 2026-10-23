#!/bin/sh
# Review-only overlays. Expected to fail at the four documented assertions
# on 8c007979; the checkout is not modified.
set -eu
review_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
review_repo=${1:-"$HOME/Source/dockhand2"}
review_repo=$(CDPATH= cd -- "$review_repo" && pwd)
review_overlay=$(mktemp /tmp/dockhand-review-overlay.XXXXXX)
trap 'rm -f "$review_overlay"' EXIT HUP INT TERM
python3 - "$review_dir" "$review_repo" "$review_overlay" <<'PY'
import json
import pathlib
import sys
evidence, repo, overlay = map(pathlib.Path, sys.argv[1:])
files = {
    'internal/command/zz_architecture_watch_overlay_test.go': 'watch_context_test.go.txt',
    'internal/engine/architecture_probe_test.go': 'engine_context_test.go.txt',
    'internal/upstream/architecture_source_identity_test.go': 'source_identity_test.go.txt',
}
overlay.write_text(json.dumps({'Replace': {str(repo / dest): str(evidence / source) for dest, source in files.items()}}))
PY
cd "$review_repo"
go test -overlay "$review_overlay" ./internal/command ./internal/engine ./internal/upstream -run '^TestArchitecture' -count=1 -timeout=90s
