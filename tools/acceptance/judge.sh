#!/usr/bin/env bash
# judge.sh answers the full stage's pending judgments: each row whose
# claim a person must judge records it as "judge pending" and the run goes
# on (lib/protocol.sh's judged), and this lists every pending claim of a
# candidate, with its evidence, and takes pass or fail for each, rewriting
# the row's result. A row the harm sweep found a blocker is one, whatever
# its claim (#133).
#
#   tools/acceptance/judge.sh --candidate <rc>                  ask for each pending claim
#   tools/acceptance/judge.sh --candidate <rc> <row> pass|fail  answer one
#   tools/acceptance/judge.sh --candidate <rc> <row> pass "<parts not exercised>"
#
# A pass records only what the person judged: where some of the claim
# wasn't exercised, those parts are named and the record says so, rather
# than the row's whole claim as held (B7 on rc4, which named a look,
# refreshes and notifications nobody saw; #232).
#
# The candidate is always named, and said in every prompt: picking the
# newest results directory by its time made a judged older candidate the
# newest, so the next answer could land on the wrong run (#230).
set -euo pipefail
: "${ACCEPT_STATE:=$HOME/.dockhand-acceptance/full}"
candidate=""
if [ "${1:-}" = --candidate ]; then
	candidate=$2
	shift 2
fi
if [ -z "$candidate" ]; then
	echo "judge.sh: name the candidate with --candidate <rc>: $(ls "$ACCEPT_STATE/results" 2>/dev/null | tr '\n' ' ')" >&2
	exit 2
fi
results="$ACCEPT_STATE/results/$candidate"
[ -n "$candidate" ] && [ -d "$results" ] || { echo "judge.sh: no results for ${candidate:-any candidate} in $ACCEPT_STATE/results" >&2; exit 1; }

# answer rewrites a pending row's result as a person judged it.
answer() {
	local file=$1 verdict=$2 unexercised=${3:-} claim
	claim=$(jq -r .why "$file")
	case "$verdict" in
	pass)
		if [ -n "$unexercised" ]; then
			jq --arg why "a person judged it holds for what was exercised; not exercised: $unexercised. The row's claim: $claim" '.result = "pass" | .why = $why | .not_exercised = $unexercised' --arg unexercised "$unexercised" "$file" >"$file.new"
		else
			jq --arg why "a person judged it holds: $claim" '.result = "pass" | .why = $why' "$file" >"$file.new"
		fi
		;;
	fail) jq --arg why "a person found it doesn't hold: $claim" '.result = "fail" | .why = $why' "$file" >"$file.new" ;;
	*) echo "judge.sh: answer pass or fail" >&2; return 2 ;;
	esac
	mv "$file.new" "$file"
	printf '%s on %s: %s\n' "$(jq -r .row "$file")" "$candidate" "$verdict"
}

if [ $# -eq 2 ] || [ $# -eq 3 ]; then
	file="$results/$1.json"
	[ -f "$file" ] && [ "$(jq -r .result "$file")" = "judge pending" ] || { echo "judge.sh: $1 has no pending judgment on $candidate" >&2; exit 1; }
	[ $# -eq 2 ] || [ "$2" = pass ] || { echo "judge.sh: only a pass names parts not exercised" >&2; exit 2; }
	answer "$file" "$2" "${3:-}"
	exit
fi
[ $# -eq 0 ] || { sed -n '9,11p' "$0" >&2; exit 2; }
echo "Judging $candidate's pending claims, in $results"
pending=0
for file in "$results"/*.json; do
	[ -f "$file" ] && [ "$(jq -r .result "$file")" = "judge pending" ] || continue
	pending=$((pending + 1))
	printf '\n%s on %s: %s\n  evidence: %s\n' "$(jq -r .row "$file")" "$candidate" "$(jq -r .why "$file")" "$(jq -r .log "$file")"
	while :; do
		printf 'Does it hold on %s? pass, fail, or skip: ' "$candidate"
		read -r verdict || exit 0
		case "$verdict" in
		pass)
			printf 'Any part of the claim not exercised? Name it, or press return for none: '
			read -r unexercised || unexercised=""
			answer "$file" pass "$unexercised"
			break
			;;
		fail) answer "$file" fail; break ;;
		skip) break ;;
		esac
	done
done
[ "$pending" -gt 0 ] || echo "judge.sh: nothing pending on $candidate"
