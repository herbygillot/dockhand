#!/bin/sh
# gate-record.sh: the record a passing gate leaves, which field testing
# reads before it builds a commit (Herby, 2026-11-27: field testing builds
# only main commits a gate passed).
#
#   tools/gate-record.sh start          prints the commit the gate checks,
#                                       or nothing where tracked files are
#                                       changed, so no commit is what it ran
#   tools/gate-record.sh pass <commit>  records the commit as gated, where
#                                       HEAD and the files are still it
#
# A record is $DOCKHAND_GATE_RECORDS/<full commit> (~/.dockhand-gates unless
# set), holding when it passed, on which Mac, and the Go it ran.
set -eu
records=${DOCKHAND_GATE_RECORDS:-$HOME/.dockhand-gates}
# clean is files that are the commit's: no tracked file changed, and no
# untracked Go file, which fmt-check and go test ./... take in (Codex's
# read of 8808d060). Other untracked files, notes beside the tree, are
# nothing the gate builds.
clean() {
	git diff --quiet HEAD -- 2>/dev/null && [ -z "$(git ls-files --others --exclude-standard -- '*.go')" ]
}
case ${1:-} in
start)
	if clean; then git rev-parse HEAD; fi
	;;
pass)
	commit=${2:-}
	if [ -z "$commit" ]; then
		echo "gate-record: the gate's files weren't a commit's; nothing recorded" >&2
		exit 0
	fi
	if [ "$(git rev-parse HEAD)" != "$commit" ] || ! clean; then
		echo "gate-record: HEAD or the files moved during the gate; $commit isn't recorded" >&2
		exit 0
	fi
	mkdir -p "$records"
	printf 'passed %s on %s with %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(uname -n)" "$(go version 2>/dev/null | cut -d' ' -f3)" >"$records/$commit"
	echo "gate-record: $commit passed the gate; $records/$commit" >&2
	;;
*)
	echo "usage: tools/gate-record.sh start | pass <commit>" >&2
	exit 2
	;;
esac
