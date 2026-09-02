#!/usr/bin/env bash
#
# liveproof drives the built dockhand over real ports and pins every
# byte it produces.
#
# The unit tests prove the pieces against synthetic Portfiles. This
# script proves the assembled binary against a real ports tree: a fixed
# list of ports, a fixed list of read-only invocations, and each one's
# stdout, stderr and exit code captured to its own file. `record`
# writes the baseline; `check` reruns and diffs every file against it,
# exiting 1 when any byte moved. Exit codes are the proof: nothing here
# counts lines or greps for success.
#
# Usage:
#   scripts/liveproof.sh record    # write the baseline
#   scripts/liveproof.sh check     # rerun and diff (what `make liveproof` runs)
#
# Environment:
#   PORTS_TREE         a macports-ports checkout (default ~/Source/macports-ports)
#   LIVEPROOF_OUT      where captures go (default .liveproof, relative to the repo)
#   LIVEPROOF_NETWORK  1 also runs the invocations that reach the network (default 0)
#   LIVEPROOF_TREE_MOVED  1 records anyway when the tree has moved past the
#                      recorded baseline (see `record` for why it refuses)
#
# Requirements: a MacPorts installation (`port version` answers), the
# checkout, and Go to build the binary. The evaluator resolves
# PortGroups through the installation's own configured tree, so
# PORTS_TREE only needs to be a checkout whose PortGroup versions that
# installation knows.
#
# Invocations, per port:
#   bump --to <current> --plan       offline: declines AlreadyCurrent (exit 10)
#                                    before any distfile would be fetched
#   bump --to <current> --diff       offline: the same decline, on the diff road
#   bump-revision --plan --reason x  offline: a plan (exit 0), or a decline (exit 10)
#                                    where the Portfile carries no revision line
#   classify                         offline: the evaluator alone
#   refresh-checksums --plan         NETWORK: fetches the port's distfiles to
#                                    compare checksums
#   bump --to <next> --plan          NETWORK: a real plan — the fetch, the
#                                    checksums, the carrier edit, any vendored
#                                    regenerator — to a version the port has
#                                    carried before, so the distfile exists
#                                    and its checksums are fixed
#   bump --to <next> --diff          NETWORK: the same plan, rendered as the patch
#
# The network invocations run only with LIVEPROOF_NETWORK=1, and their
# output is only as stable as what upstream serves: a distfile rerolled
# upstream, or a `next` withdrawn, is a re-record, not a regression.
# A baseline is complete only when its meta says network: 1 — recorded
# offline, the checksum road has no baseline at all, and `check` says
# so.
#
# The bump versions are pinned in the table below and checked against
# the Portfile before anything runs. A bump to a version the port does
# not carry would fetch the new distfile, and the offline guarantee
# would quietly stop holding; so when the tree moves past a pinned
# version the script stops and says so. Update the table and re-record.
#
# Captures name the ports tree as <tree>, never by its absolute path,
# so a baseline recorded against one checkout compares against another
# at the same revision. The `meta` file records the real path and the
# revision; `check` reports a moved tree as context for a diff, not as
# a failure by itself. `record` also mirrors manifest.sha256 and meta
# to scripts/liveproof/, the tracked copy: the baseline is then a fact
# of the repository, and a machine with no captures of its own can
# still `check` its rerun against the recorded hashes.
#
# Exit status: 0 recorded, or every file matched; 1 a difference;
# 2 usage or a stale port table; 3 no MacPorts or no ports tree.

set -euo pipefail

die() { # status message
	echo "liveproof: $2" >&2
	exit "$1"
}

mode=${1:-}
case $mode in
record | check) ;;
*) die 2 "usage: $0 record|check" ;;
esac

repo=$(cd -P "$(dirname "$0")/.." && pwd)
cd "$repo"

# The fixed list: name|portdir|version|next. name is the capture's
# file-name stem, portdir is tree-relative, version is what the
# Portfile's version carrier says today, and next is a version the port
# has carried before — from the tree's own git log for the portdir, so
# the distfile is real and its checksums are fixed — that the network
# bump verbs bump to. Empty when the log holds no other version; the
# two verbs are then skipped for that port. One port per style the tree
# is made of: a plain github port, a cargo port that is also a github
# one, a perl5 port (transformed carrier, subports, a revision line), an
# R port, a second cargo port, and a go2port-shaped port with a
# go.vendors block and no revision line.
ports='
jq|sysutils/jq|1.8.2|1.8.1
ruff|devel/ruff|0.16.5|0.16.4
p5-algorithm-annotate|perl/p5-algorithm-annotate|0.10|
R-AER|R/R-AER|1.2-14|1.2-13
ast-grep|devel/ast-grep|0.45.3|0.45.2
neo-cowsay|textproc/neo-cowsay|2.0.4|
'

verbs='bump-plan bump-diff bump-revision-plan classify refresh-checksums-plan bump-plan-next bump-diff-next'

# needs_network names the verbs that reach past the machine.
needs_network() { # verb
	case $1 in
	refresh-checksums-plan | bump-plan-next | bump-diff-next) return 0 ;;
	*) return 1 ;;
	esac
}

# gated_offline says whether a capture's name belongs to a network verb
# this run did not perform: not compared, rather than stale or missing.
gated_offline() { # capture-name
	local verb
	[[ $LIVEPROOF_NETWORK != 1 ]] || return 1
	for verb in $verbs; do
		if needs_network "$verb" && [[ $1 == *-"$verb".* ]]; then
			return 0
		fi
	done
	return 1
}

# --- the machine ---

PORTS_TREE=${PORTS_TREE:-$HOME/Source/macports-ports}
[[ -d $PORTS_TREE ]] || die 3 "no ports tree at $PORTS_TREE (set PORTS_TREE to a macports-ports checkout)"
PORTS_TREE=$(cd -P "$PORTS_TREE" && pwd)
export PORTS_TREE

port_bin=$(command -v port || true)
[[ -n $port_bin ]] || port_bin=/opt/local/bin/port
macports_version=$("$port_bin" version 2>/dev/null) || die 3 "MacPorts is not installed (\`port version\` failed); the evaluator needs it"

LIVEPROOF_NETWORK=${LIVEPROOF_NETWORK:-0}
out=${LIVEPROOF_OUT:-.liveproof}
case $out in
/*) ;;
*) out=$repo/$out ;;
esac
mirror=$repo/scripts/liveproof

if tree_head=$(git -C "$PORTS_TREE" rev-parse HEAD 2>/dev/null); then
	tree_dirty=$(git -C "$PORTS_TREE" status --short -- $(printf '%s\n' "$ports" | awk -F'|' 'NF == 4 { print $2 }') | tr '\n' ' ')
else
	tree_head="not a git checkout"
	tree_dirty=""
fi

# carries_version says whether a Portfile's version-carrier line names
# the pinned version: a `version` line, or any `<group>.setup` line
# (github.setup, go.setup, perl5.setup, R.setup) whose arguments hold
# it as a whole field. A match anywhere else -- a checksum, a comment --
# proves nothing about what a bump would do. Fields are compared as
# strings, so the version needs no escaping.
carries_version() { # portfile version
	awk -v v="$2" '
		$1 == "version" || $1 ~ /^[A-Za-z0-9_.]+\.setup$/ {
			for (i = 2; i <= NF; i++) if ($i == v) found = 1
		}
		END { exit !found }' "$1"
}

# each_port runs a function over every table row: f name portdir
# version next. Its locals are named apart from every callee's,
# because bash scopes dynamically: a callee reading an unset `dir`
# would see this one.
each_port() { # f
	local row_name row_dir row_version row_next
	while IFS='|' read -r row_name row_dir row_version row_next; do
		[[ -n $row_name ]] || continue
		"$1" "$row_name" "$row_dir" "$row_version" "$row_next"
	done <<-EOF
	$ports
	EOF
}

check_port() { # name portdir version next
	local portfile=$PORTS_TREE/$2/Portfile
	[[ -f $portfile ]] || die 3 "$2 has no Portfile under $PORTS_TREE"
	carries_version "$portfile" "$3" ||
		die 2 "$2 no longer carries version $3 (tree at $tree_head); update the table in scripts/liveproof.sh and re-record"
	# A next equal to the current version would decline AlreadyCurrent
	# and pin nothing the offline verbs do not.
	[[ $4 != "$3" ]] || die 2 "$2: next $4 is the current version; pick one the port has moved away from"
}

each_port check_port

# --- the binary ---

make build

# --- capturing ---

# tree_pattern is PORTS_TREE as a sed basic regular expression, so a
# path holding a metacharacter still matches itself and nothing else.
tree_pattern=$(printf '%s' "$PORTS_TREE" | sed 's/[][\.*^$]/\\&/g')

# capture runs one invocation and lands its three files, with the ports
# tree's path rewritten to <tree> in both streams.
capture() { # dir stem args...
	local dir=$1 stem=$2 code=0 f
	shift 2
	env DOCKHAND_TREE="$PORTS_TREE" "$repo/dockhand" "$@" >"$dir/$stem.out" 2>"$dir/$stem.err" || code=$?
	for f in "$dir/$stem.out" "$dir/$stem.err"; do
		sed "s|$tree_pattern|<tree>|g" "$f" >"$f.tmp" && mv "$f.tmp" "$f"
	done
	printf '%s\n' "$code" >"$dir/$stem.code"
	printf '  %-48s exit %s\n' "$stem" "$code"
}

# run_verb maps a verb to its dockhand arguments for one port.
run_verb() { # dir name verb version next portdir
	local dir=$1 name=$2 verb=$3 version=$4 next=$5 portdir=$6
	if needs_network "$verb" && [[ $LIVEPROOF_NETWORK != 1 ]]; then
		printf '  %-48s skipped (needs the network; LIVEPROOF_NETWORK=1 runs it)\n' "$name-$verb"
		return 0
	fi
	case $verb in
	bump-plan) capture "$dir" "$name-$verb" bump --to "$version" --plan "$portdir" ;;
	bump-diff) capture "$dir" "$name-$verb" bump --to "$version" --diff "$portdir" ;;
	bump-revision-plan) capture "$dir" "$name-$verb" bump-revision --plan --reason x "$portdir" ;;
	classify) capture "$dir" "$name-$verb" classify "$portdir" ;;
	refresh-checksums-plan) capture "$dir" "$name-$verb" refresh-checksums --plan "$portdir" ;;
	bump-plan-next | bump-diff-next)
		if [[ -z $next ]]; then
			printf '  %-48s skipped (the table names no earlier version for %s)\n' "$name-$verb" "$name"
			return 0
		fi
		case $verb in
		bump-plan-next) capture "$dir" "$name-$verb" bump --to "$next" --plan "$portdir" ;;
		bump-diff-next) capture "$dir" "$name-$verb" bump --to "$next" --diff "$portdir" ;;
		esac
		;;
	*) die 2 "unknown verb $verb" ;;
	esac
}

run_all() { # dir
	local capture_dir=$1
	run_port() { # name portdir version next
		local verb
		for verb in $verbs; do
			run_verb "$capture_dir" "$1" "$verb" "$3" "$4" "$PORTS_TREE/$2"
		done
	}
	each_port run_port
}

# sha256 tooling: shasum ships with macOS and MacPorts; sha256sum is
# the coreutils spelling. Both print "<hash>  <name>" and both check.
if command -v shasum >/dev/null 2>&1; then
	sha256_sum() { shasum -a 256 "$@"; }
	sha256_check() { shasum -a 256 --status -c "$@"; }
else
	sha256_sum() { sha256sum "$@"; }
	sha256_check() { sha256sum --status -c "$@"; }
fi

# write_manifest hashes every capture in a directory, in a fixed order.
write_manifest() { # dir
	(
		cd "$1"
		ls ./*.out ./*.err ./*.code | sed 's|^\./||' | LC_ALL=C sort | while read -r f; do
			sha256_sum "$f"
		done
	) >"$1/manifest.sha256"
}

write_meta() { # dir
	{
		echo "dockhand: $("$repo/dockhand" --version)"
		echo "macports: $macports_version"
		echo "ports_tree: $PORTS_TREE"
		echo "ports_tree_head: $tree_head"
		echo "ports_tree_dirty: ${tree_dirty:-none}"
		echo "network: $LIVEPROOF_NETWORK"
	} >"$1/meta"
}

# manifest_hash reads one file's recorded hash out of a manifest, or
# nothing when the manifest does not name it.
manifest_hash() { # manifest name
	awk -v n="$2" '$2 == n { print $1 }' "$1"
}

# --- comparing ---

compared=0
differ=0

# compare_captures diffs a rerun against a directory of captures, byte
# for byte, showing every difference as a diff.
compare_captures() { # baseline-dir rerun-dir
	local base=$1 rerun=$2 f name
	for f in "$rerun"/*.out "$rerun"/*.err "$rerun"/*.code; do
		name=${f##*/}
		compared=$((compared + 1))
		if [[ ! -f $base/$name ]]; then
			echo "liveproof: $name: no baseline (re-record; LIVEPROOF_NETWORK=1 if it is network-gated)"
			differ=$((differ + 1))
			continue
		fi
		if ! cmp -s "$base/$name" "$f"; then
			echo "liveproof: $name differs from the baseline"
			diff -u "$base/$name" "$f" || true
			differ=$((differ + 1))
		fi
	done
	# A baseline capture this run did not produce is a stale baseline,
	# unless it is network-gated and this run stayed offline.
	for f in "$base"/*.out "$base"/*.err "$base"/*.code; do
		name=${f##*/}
		[[ -f $rerun/$name ]] && continue
		if gated_offline "$name"; then
			echo "liveproof: $name: not compared (network-gated; LIVEPROOF_NETWORK=1 compares it)"
		else
			echo "liveproof: $name: in the baseline, but this run did not produce it (re-record)"
			differ=$((differ + 1))
		fi
	done
}

# compare_manifests holds a rerun's manifest against a recorded one
# when the captures behind the record are not at hand: a moved hash is
# a difference it can name but not show.
compare_manifests() { # baseline-manifest rerun-manifest
	local base=$1 rerun=$2 name hash want
	while read -r hash name; do
		compared=$((compared + 1))
		want=$(manifest_hash "$base" "$name")
		if [[ -z $want ]]; then
			echo "liveproof: $name: no baseline (re-record; LIVEPROOF_NETWORK=1 if it is network-gated)"
			differ=$((differ + 1))
		elif [[ $want != "$hash" ]]; then
			echo "liveproof: $name differs from the recorded baseline (hash $want, now $hash); record locally to see the diff"
			differ=$((differ + 1))
		fi
	done <"$rerun"
	while read -r hash name; do
		[[ -n $(manifest_hash "$rerun" "$name") ]] && continue
		if gated_offline "$name"; then
			echo "liveproof: $name: not compared (network-gated; LIVEPROOF_NETWORK=1 compares it)"
		else
			echo "liveproof: $name: in the baseline, but this run did not produce it (re-record)"
			differ=$((differ + 1))
		fi
	done <"$base"
}

# --- modes ---

record() {
	# A re-record against a moved tree bakes two kinds of difference
	# into one new baseline: the change being proved, and whatever
	# upstream did to these ports in the meantime. The hashes cannot
	# tell them apart afterwards, so the refusal is here, where the
	# person doing it can still choose — check out the recorded
	# revision, or say the move is intended and be able to name what it
	# brought. `check` reports the same fact as context for a diff; at
	# record time it decides whether the baseline means anything.
	local recorded_head
	recorded_head=$(sed -n 's/^ports_tree_head: //p' "$mirror/meta" 2>/dev/null || true)
	if [[ -n $recorded_head && $recorded_head != "$tree_head" && ${LIVEPROOF_TREE_MOVED:-0} != 1 ]]; then
		die 2 "the recorded baseline is at tree $recorded_head and this tree is at $tree_head;
  a re-record here mixes upstream's changes into the diff. Check out $recorded_head,
  or re-record with LIVEPROOF_TREE_MOVED=1 and say in the commit what the move brought"
	fi
	mkdir -p "$out"
	# Only what this script wrote is removed: LIVEPROOF_OUT may be
	# anywhere the user pointed it. A check's own rerun directory is
	# its to remove, so only the fixed path older versions left behind
	# is swept here.
	rm -rf "$out/rerun"
	rm -f "$out"/*.out "$out"/*.err "$out"/*.code "$out/manifest.sha256" "$out/meta"
	echo "liveproof: recording into $out (tree at $tree_head)"
	run_all "$out"
	write_manifest "$out"
	write_meta "$out"
	mkdir -p "$mirror"
	cp "$out/manifest.sha256" "$out/meta" "$mirror/"
	echo "liveproof: recorded $(wc -l <"$out/manifest.sha256" | tr -d ' ') files; manifest at $out/manifest.sha256, mirrored to $mirror/"
	[[ $LIVEPROOF_NETWORK == 1 ]] || echo "liveproof: note: recorded offline; the network verbs have no baseline until \`LIVEPROOF_NETWORK=1 $0 record\`"
}

check() {
	local meta=""
	if [[ -f $out/manifest.sha256 ]]; then
		# The baseline must still be what was recorded, or the diff
		# below would compare against an edited or truncated reference.
		(cd "$out" && sha256_check manifest.sha256) || die 1 "the baseline in $out no longer matches its own manifest; re-record"
		meta=$out/meta
	elif [[ -f $mirror/manifest.sha256 ]]; then
		echo "liveproof: no captures in $out; comparing hashes against the recorded $mirror/manifest.sha256"
		meta=$mirror/meta
	else
		die 2 "no baseline in $out and none recorded in $mirror; run \`$0 record\` first"
	fi

	# The rerun directory is unique per invocation, and removed on the
	# way out. A fixed path was a trap once two agents shared the
	# checkout: a second `check` starting mid-compare wiped the first
	# one's captures, which showed up as two files arriving empty and
	# read exactly like a parity regression. Every difference is printed
	# as it is found, so nothing is lost by not keeping the directory;
	# a run killed before its trap leaves one behind, under a name no
	# other run will reuse.
	#
	# rerun_dir is deliberately not local: the trap fires after this
	# function has returned, and a local would be gone by then.
	mkdir -p "$out"
	rerun_dir=$(mktemp -d "$out/rerun.XXXXXX")
	trap 'rm -rf "$rerun_dir"' EXIT
	echo "liveproof: rerunning into $rerun_dir (tree at $tree_head)"
	run_all "$rerun_dir"
	write_manifest "$rerun_dir"
	write_meta "$rerun_dir"

	if [[ -f $out/manifest.sha256 ]]; then
		compare_captures "$out" "$rerun_dir"
	else
		compare_manifests "$mirror/manifest.sha256" "$rerun_dir/manifest.sha256"
	fi

	local recorded_head recorded_network
	recorded_head=$(sed -n 's/^ports_tree_head: //p' "$meta" 2>/dev/null || true)
	if [[ -n $recorded_head && $recorded_head != "$tree_head" ]]; then
		echo "liveproof: note: the baseline was recorded at tree $recorded_head; this run is at $tree_head"
	fi
	recorded_network=$(sed -n 's/^network: //p' "$meta" 2>/dev/null || true)
	if [[ $recorded_network != 1 ]]; then
		echo "liveproof: note: the baseline was recorded offline; the network verbs have no baseline (\`LIVEPROOF_NETWORK=1 $0 record\`)"
	fi
	[[ -z $tree_dirty ]] || echo "liveproof: note: uncommitted changes in the tree: $tree_dirty"

	echo "liveproof: $compared files compared, $differ differ"
	[[ $differ -eq 0 ]]
}

"$mode"
