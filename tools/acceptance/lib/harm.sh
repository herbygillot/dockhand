# shellcheck shell=bash
# The harm sweep: prime-time.md's eight invariants, H1 to H8, checked by
# snapshotting before a row acts and again after. Each check writes
# $ROW_DIR/harm/H<n>: "ok", "broken: <what>", or "skipped: <why>".
#
# What it watches comes from the environment the stage sets up:
#   ACCEPT_WATCH          the ports checkouts, space-separated (H1, H2, H5)
#   ACCEPT_UPSTREAM       remotes that are upstream, not the fork (H2)
#   ACCEPT_GH_LOGIN       the GitHub login whose pull requests count (H3)
#   ACCEPT_SECRET_DIRS    where a token mustn't be written (H4)
#   ACCEPT_RUN_DIR        where Next: lines run from (H6)

# The token prefixes GitHub gives, and a fine-grained token's.
HARM_TOKEN='(gh[opsur]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})'

# The verbs whose --plan changes nothing, and those that only read.
HARM_PLAN_VERBS=" update checksums revbump check tidy submit "
HARM_READ_VERBS=" status diff impact path logs queue outdated config explain "

harm_id() { printf '%s' "$1" | shasum | cut -c1-12; }

# harm_allowed says whether a value matches any glob in an allowance file.
harm_allowed() {
	local file=$1 value=$2 glob
	[ -f "$file" ] || return 1
	while IFS= read -r glob; do
		[ -n "$glob" ] || continue
		# shellcheck disable=SC2254
		case "$value" in $glob) return 0 ;; esac
	done <"$file"
	return 1
}

# harm_snapshot records what the invariants compare, in a directory.
harm_snapshot() {
	local out=$1 repo id wt
	mkdir -p "$out"
	for repo in $ACCEPT_WATCH; do
		id=$(harm_id "$repo")
		git -C "$repo" for-each-ref --format='%(refname) %(objectname)' >"$out/refs.$id" 2>/dev/null || :
		git -C "$repo" worktree list --porcelain 2>/dev/null | sed -n 's/^worktree //p' >"$out/worktrees.$id"
		: >"$out/files.$id"
		while IFS= read -r wt; do
			[ -d "$wt" ] || continue
			harm_work_files "$wt" >>"$out/files.$id"
		done <"$out/worktrees.$id"
		harm_remote_refs "$repo" >"$out/remote.$id" 2>"$out/remote.$id.err" || :
	done
	harm_prs >"$out/prs" 2>/dev/null || :
	harm_running >"$out/running" 2>/dev/null || :
}

# harm_work_files lists a worktree's work that its commits don't hold, its
# edited and untracked files, each with its content's hash.
harm_work_files() {
	local wt=$1 line path skip=0 hash
	(cd "$wt" && git status --porcelain=v1 -z --untracked-files=all 2>/dev/null) | tr '\0' '\n' |
		while IFS= read -r line; do
			if [ "$skip" -eq 1 ]; then
				skip=0
				continue
			fi
			[ -n "$line" ] || continue
			path=${line:3}
			case "${line:0:1}" in R | C) skip=1 ;; esac
			if [ -f "$wt/$path" ]; then
				hash=$(shasum -a 256 "$wt/$path" | cut -d' ' -f1)
			else
				hash=deleted
			fi
			printf '%s\t%s\t%s\n' "$wt" "$path" "$hash"
		done
}

# harm_remote_refs lists each fork remote's refs, as the fork has them.
harm_remote_refs() {
	local repo=$1 remote
	for remote in $(git -C "$repo" remote); do
		case " ${ACCEPT_UPSTREAM:-} " in *" $remote "*) continue ;; esac
		git -C "$repo" ls-remote --heads --tags "$remote" | sed "s|^|$remote |" || return 1
	done
}

# harm_prs lists the pull requests the login has opened, where one's set.
harm_prs() {
	[ -n "${ACCEPT_GH_LOGIN:-}" ] && command -v gh >/dev/null || return 1
	gh pr list --repo macports/macports-ports --author "$ACCEPT_GH_LOGIN" --state all --limit 200 --json number --jq '.[].number' | sort
}

# harm_running lists the Tart VMs running and the checks queued or running.
harm_running() {
	if command -v tart >/dev/null; then
		tart list --format json 2>/dev/null | jq -r '.[] | select(.State == "running") | "vm " + .Name' 2>/dev/null || :
	fi
	"$DH_BIN" --json queue 2>/dev/null | jq -r '.result.runs[]? | select(.state == "queued" or .state == "running") | "run " + .name' 2>/dev/null || :
}

harm_write() { printf '%s\n' "$2" >"$ROW_DIR/harm/$1"; }

# harm_check compares the snapshots and runs what needs running, writing
# each invariant's verdict.
harm_check() {
	local before=$ROW_DIR/before after=$ROW_DIR/after
	mkdir -p "$ROW_DIR/harm"
	harm_h1 "$before" "$after"
	harm_h2 "$before" "$after"
	harm_h3 "$before" "$after"
	harm_h4
	harm_h5
	harm_h6
	harm_h7 "$before" "$after"
	harm_h8
}

# H1: no work lost. Every edited or untracked file is still there with
# its content, every commit a ref named is still an object, and no ref
# went, but where the row says it may.
harm_h1() {
	local before=$1 after=$2 repo id wt path hash ref object broken=""
	for repo in $ACCEPT_WATCH; do
		id=$(harm_id "$repo")
		while IFS="$(printf '\t')" read -r wt path hash; do
			[ "$hash" = deleted ] && continue
			if [ -f "$wt/$path" ] && [ "$(shasum -a 256 "$wt/$path" | cut -d' ' -f1)" = "$hash" ]; then
				continue
			fi
			harm_allowed "$ROW_DIR/allow.change" "$wt/$path" && continue
			harm_allowed "$ROW_DIR/allow.change" "$path" && continue
			broken="$broken $wt/$path"
		done <"$before/files.$id"
		while read -r ref object; do
			if ! grep -q "^$ref " "$after/refs.$id" && ! harm_allowed "$ROW_DIR/allow.refgone" "$ref"; then
				broken="$broken ref:$ref"
			fi
			git -C "$repo" cat-file -e "$object" 2>/dev/null || broken="$broken commit:$object"
		done <"$before/refs.$id"
	done
	if [ -n "$broken" ]; then
		harm_write H1 "broken: lost${broken}"
	else
		harm_write H1 ok
	fi
}

# H2: nothing pushed but what the row says it pushes.
harm_h2() {
	local before=$1 after=$2 repo id line ref broken="" read=""
	for repo in $ACCEPT_WATCH; do
		id=$(harm_id "$repo")
		if [ -s "$before/remote.$id.err" ] || [ -s "$after/remote.$id.err" ]; then
			harm_write H2 "skipped: a fork remote couldn't be read: $(head -1 "$before/remote.$id.err" "$after/remote.$id.err" 2>/dev/null | grep -v '^==>' | head -1)"
			return
		fi
		read=yes
		while IFS= read -r line; do
			grep -qxF "$line" "$before/remote.$id" && continue
			ref=$(printf '%s' "$line" | awk '{print $1 " " $3}')
			harm_allowed "$ROW_DIR/allow.push" "$ref" && continue
			broken="$broken pushed:$ref"
		done <"$after/remote.$id"
		while IFS= read -r line; do
			ref=$(printf '%s' "$line" | awk '{print $1 " " $3}')
			awk -v r="$(printf '%s' "$line" | awk '{print $3}')" -v m="$(printf '%s' "$line" | awk '{print $1}')" '$1 == m && $3 == r {found=1} END {exit !found}' "$after/remote.$id" && continue
			harm_allowed "$ROW_DIR/allow.push" "$ref" && continue
			broken="$broken removed:$ref"
		done <"$before/remote.$id"
	done
	if [ -z "$read" ]; then
		harm_write H2 "skipped: nothing to watch"
	elif [ -n "$broken" ]; then
		harm_write H2 "broken:$broken"
	else
		harm_write H2 ok
	fi
}

# H3: no pull request opened but those the row may open.
harm_h3() {
	local before=$1 after=$2 opened allowed=0
	if [ -z "${ACCEPT_GH_LOGIN:-}" ]; then
		harm_write H3 "skipped: no GitHub login to watch, as in the quick stage, which opens none"
		return
	fi
	[ -f "$ROW_DIR/allow.prs" ] && allowed=$(cat "$ROW_DIR/allow.prs")
	opened=$(comm -13 "$before/prs" "$after/prs" | grep -c . || :)
	if [ "$opened" -gt "$allowed" ]; then
		harm_write H3 "broken: $opened pull requests opened, where $allowed may be: $(comm -13 "$before/prs" "$after/prs" | tr '\n' ' ')"
	else
		harm_write H3 ok
	fi
}

# H4: no secret written where it shouldn't be.
harm_h4() {
	local found dir
	found=$(grep -rlaE "$HARM_TOKEN" "$ROW_DIR/out.log" "$ROW_DIR/json" 2>/dev/null || :)
	for dir in ${ACCEPT_SECRET_DIRS:-}; do
		[ -e "$dir" ] || continue
		found="$found $(grep -rlaE "$HARM_TOKEN" "$dir" 2>/dev/null | tr '\n' ' ')"
	done
	found=$(printf '%s' "$found" | tr -s ' ')
	if [ -n "${found// /}" ]; then
		harm_write H4 "broken: a token in$found"
	else
		harm_write H4 ok
	fi
}

# H5: status tells the truth about each branch's head and edits.
harm_h5() {
	local repo json broken="" name gitbranch head worktree edited actual
	repo=${ACCEPT_WATCH%% *}
	if ! json=$(cd "${ACCEPT_RUN_DIR:-$repo}" && "$DH_BIN" --json status 2>/dev/null); then
		harm_write H5 "broken: status failed: $(printf '%s' "$json" | jq -r '.error // empty' 2>/dev/null)"
		return
	fi
	while IFS="$(printf '\t')" read -r name gitbranch head worktree edited; do
		[ -n "$name" ] || continue
		actual=$(git -C "$repo" rev-parse --verify -q "refs/heads/$gitbranch" 2>/dev/null || :)
		if [ -n "$actual" ] && [ "$actual" != "$head" ]; then
			broken="$broken $name:head"
		fi
		if [ -d "$worktree" ]; then
			actual=$(cd "$worktree" && git status --porcelain=v1 --untracked-files=no | cut -c4- | sort | tr '\n' ',' | sed 's/,$//')
			[ "$actual" = "$edited" ] || broken="$broken $name:edits($edited|$actual)"
		fi
	done <<EOT
$(printf '%s' "$json" | jq -r '.result.branches[]? | select(.missing != true) | [.name, .git_branch, .head, .worktree, ((.edited // []) | sort | join(","))] | @tsv')
EOT
	if [ -n "$broken" ]; then
		harm_write H5 "broken:$broken"
	else
		harm_write H5 ok
	fi
}

# H6: every Next: line works: each step it names, run as its --plan where
# it would change something, or as named where it only reads, is
# accepted. A step that can't be run from here is said, not counted.
harm_h6() {
	local line segment dir broken="" unverified="" ran=0 status verb word words
	dir=${ACCEPT_RUN_DIR:-${ACCEPT_WATCH%% *}}
	: >"$ROW_DIR/next.log"
	while IFS= read -r line; do
		line=${line#Next: }
		line=$(printf '%s\n' "$line" | awk '{gsub(/, then |; |, or /, "\n"); print}')
		while IFS= read -r segment; do
			segment=$(printf '%s' "$segment" | sed 's/^ *//; s/ *$//; s/[.]$//')
			case "$segment" in
			'cd "$(dockhand path '*')"')
				word=${segment#cd \"\$(dockhand path }
				word=${word%)\"}
				dir=$("$DH_BIN" path "$word" 2>/dev/null) || {
					broken="$broken [$segment]"
					continue
				}
				continue
				;;
			dockhand\ *) ;;
			*)
				unverified="$unverified [$segment]"
				continue
				;;
			esac
			case "$segment" in *'<'* | *'"'* | *"'"* | *'$'* | *'`'*)
				unverified="$unverified [$segment]"
				continue
				;;
			esac
			words=${segment#dockhand }
			verb=""
			for word in $words; do
				case "$word" in -*) ;; *)
					verb=$word
					break
					;;
				esac
			done
			case "$HARM_PLAN_VERBS" in *" $verb "*)
				case " $words " in *" --plan "*) ;; *) words="$words --plan" ;; esac
				;;
			*) case "$HARM_READ_VERBS" in *" $verb "*) ;; *)
				unverified="$unverified [$segment]"
				continue
				;;
				esac ;;
			esac
			status=0
			# shellcheck disable=SC2086
			(cd "$dir" && "$DH_BIN" $words) >>"$ROW_DIR/next.log" 2>&1 </dev/null || status=$?
			ran=$((ran + 1))
			case "$status" in 0 | 3) ;; *) broken="$broken [dockhand $words: exit $status]" ;; esac
		done <<EOT
$line
EOT
	done <<EOT
$(grep -h '^Next: ' "$ROW_DIR/out.log" 2>/dev/null)
EOT
	if [ -n "$unverified" ]; then
		printf '%s\n' "$unverified" >"$ROW_DIR/next.unverified"
	fi
	if [ -n "$broken" ]; then
		harm_write H6 "broken: refused$broken"
	else
		harm_write H6 "ok: $ran run"
	fi
}

# H7: nothing left running that the row didn't mean to leave.
harm_h7() {
	local before=$1 after=$2 line broken=""
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		grep -qxF "$line" "$before/running" && continue
		harm_allowed "$ROW_DIR/allow.running" "$line" && continue
		broken="$broken [$line]"
	done <"$after/running"
	if [ -n "$broken" ]; then
		harm_write H7 "broken: left running$broken"
	else
		harm_write H7 ok
	fi
}

# H8: scripts can trust the output: each --json envelope parses, and its
# exit_code is the process's, one of usage.md's.
harm_h8() {
	local file exit broken="" n=0
	for file in "$ROW_DIR"/json/*.json; do
		[ -f "$file" ] || continue
		n=$((n + 1))
		exit=$(cat "$file.exit")
		if ! jq -e '.version == 1 and (.exit_code | type == "number") and has("command") and has("result")' "$file" >/dev/null 2>&1; then
			broken="$broken [$(cat "$file.args"): not an envelope]"
			continue
		fi
		[ "$(jq -r .exit_code "$file")" = "$exit" ] || broken="$broken [$(cat "$file.args"): envelope says $(jq -r .exit_code "$file"), process $exit]"
		case "$exit" in 0 | 1 | 2 | 3 | 130) ;; *) broken="$broken [$(cat "$file.args"): exit $exit]" ;; esac
	done
	if [ -n "$broken" ]; then
		harm_write H8 "broken:$broken"
	else
		harm_write H8 "ok: $n checked"
	fi
}
