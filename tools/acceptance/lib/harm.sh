# shellcheck shell=bash
# The harm sweep: prime-time.md's eight invariants, H1 to H8, and the
# quick stage's own, H9, checked by
# snapshotting before a row acts and again after. Each check writes
# $ROW_DIR/harm/H<n>: "ok", "broken: <what>", or "skipped: <why>".
#
# What it watches comes from the environment the stage sets up:
#   ACCEPT_WATCH          the ports checkouts, space-separated (H1, H2, H5)
#   ACCEPT_UPSTREAM       remotes that are upstream, not the fork (H2)
#   ACCEPT_GH_LOGIN       the GitHub login whose pull requests count (H3)
#   ACCEPT_SECRET_DIRS    where a token mustn't be written (H4)
#   ACCEPT_RUN_DIR        where Next: lines run from (H6)
#   ACCEPT_HOME_DIRS      a person's own state the stage mustn't touch (H9)

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
	# A listing that fails says so in prs.err, which H3 reads: dhtest's gh,
	# never logged in, made H3 pass while A4 opened a pull request (the
	# rc3 full run, 2026-10-06).
	harm_prs >"$out/prs" 2>"$out/prs.err" || printf 'the pull requests could not be listed\n' >>"$out/prs.err"
	harm_running >"$out/running" 2>/dev/null || :
	harm_home >"$out/home"
	harm_tokens >"$out/tokens" 2>/dev/null || :
}

# harm_tokens lists, as file and the token-shaped text found in it, every
# token-shaped string in the secret directories (H4's), sorted: H4 flags
# one the row added, beside one the row's own output shows, so what was
# there before the row, a person's or an image's, is no harm of its, and a
# new one anywhere it searches is.
harm_tokens() {
	local dir
	for dir in ${ACCEPT_SECRET_DIRS:-}; do
		[ -e "$dir" ] || continue
		grep -roaE --exclude-dir=tart --exclude-dir=archives --exclude='*.img' --exclude='*.tbz2' "$HARM_TOKEN" "$dir" 2>/dev/null || :
	done | sort -u
}

# harm_home lists the person's own state directories: whether each is
# there, and each file in it with its size and modification time.
harm_home() {
	local dir
	for dir in ${ACCEPT_HOME_DIRS:-}; do
		if [ ! -e "$dir" ]; then
			printf 'absent %s\n' "$dir"
			continue
		fi
		printf 'present %s\n' "$dir"
		find "$dir" \( -type f -o -type l \) -exec stat -f 'file %N %z %m' {} + 2>/dev/null | sort
	done
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

# harm_prs lists the pull requests the login has opened, where one's set,
# at MacPorts and in the sandbox the full stage's test ones go to.
harm_prs() {
	[ -n "${ACCEPT_GH_LOGIN:-}" ] && command -v gh >/dev/null || return 1
	local repo
	for repo in macports/macports-ports ${DOCKHAND_PULL_REQUESTS:-}; do
		gh pr list --repo "$repo" --author "$ACCEPT_GH_LOGIN" --state all --limit 200 --json number --jq ".[] | \"$repo#\(.number)\"" || return 1
	done | sort
}

# harm_running lists the Tart VMs running and the checks queued or running.
harm_running() {
	local home
	if command -v tart >/dev/null; then
		# dockhand's Tart home and Tart's own, each listed only where it's
		# there, since listing makes one.
		for home in "${DOCKHAND_TART_HOME:-$HOME/.dockhand/tart}" "${TART_HOME:-$HOME/.tart}"; do
			[ -d "$home" ] || continue
			TART_HOME=$home tart list --format json 2>/dev/null | jq -r '.[] | select(.State == "running") | "vm " + .Name' 2>/dev/null || :
		done
	fi
	"$DH_BIN" --json queue 2>/dev/null | jq -r '.result.runs[]? | select(.state == "queued" or .state == "running") | "run " + .name' 2>/dev/null || :
	# The fault kit's own processes, a row's proxy among them.
	if [ -n "${ACCEPT_STATE:-}" ]; then
		pgrep -f "$ACCEPT_STATE/bin/faultproxy" 2>/dev/null | sed 's/^/process faultproxy /' || :
	fi
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
	harm_h9 "$before" "$after"
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
	if [ -s "$before/prs.err" ] || [ -s "$after/prs.err" ]; then
		harm_write H3 "not checked: $(cat "$before/prs.err" "$after/prs.err" 2>/dev/null | grep . | head -1)"
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
	# Tart's VM images and the archives guests built are binary, gigabytes
	# of them: a token's shape turns up in their bytes by chance, as it did
	# in three disk.img files of the Golden Gate vanilla image before any
	# token was on the host (the rc1 full stage). Nothing of the host's
	# login goes into a guest or its archives (dockhand gives a guest the
	# revision, its targets, and kept archives, never a credential), so
	# they're left out; dockhand's own state beside them, its database,
	# configuration, logs, and the logs and results copied out of guests,
	# is searched as before.
	# A token-shaped string the row added to the secret directories, in a
	# file or a value, compared with what was there before it began
	# (harm_tokens): one that was already there, a person's or an image's,
	# is the row's no more than a disk image's bytes are, and a new one is,
	# wherever it is searched.
	if [ -f "$ROW_DIR/before/tokens" ] && [ -f "$ROW_DIR/after/tokens" ]; then
		found="$found $(comm -13 "$ROW_DIR/before/tokens" "$ROW_DIR/after/tokens" | cut -d: -f1 | sort -u | tr '\n' ' ')"
	else
		for dir in ${ACCEPT_SECRET_DIRS:-}; do
			[ -e "$dir" ] || continue
			found="$found $(grep -rlaE --exclude-dir=tart --exclude-dir=archives --exclude='*.img' --exclude='*.tbz2' "$HARM_TOKEN" "$dir" 2>/dev/null | tr '\n' ' ')"
		done
	fi
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
	# In the full stage A0 runs before A1 installs the dockhand under
	# test, whose status isn't there to read: nothing to judge, not a
	# harm (the rc1 full stage on the M1).
	if [ ! -x "${DH_BIN:-}" ]; then
		harm_write H5 "skipped: no dockhand at $DH_BIN yet to read status with"
		return
	fi
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
			# What a step says in parentheses is for the reader, not the
			# command: "dockhand check (the files changed)".
			segment=$(printf '%s' "$segment" | sed 's/ *([^()]*)$//; s/^ *//; s/ *$//; s/[.]$//')
			[ -n "$segment" ] || continue
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
			# One that asks GitHub of every port a maintainer has, about
			# three requests a port, isn't run: setup's Next: names
			# outdated --mine, and H6 ran it after each of A10's setups,
			# over Herby's 320 ports, which spent the stage's hourly
			# allowance (the M1's runs at 10aac0c3 and 1da4fdbf).
			case " $words " in *" --mine "* | *" --all "*)
				unverified="$unverified [$segment: asks GitHub of every port, so it isn't run]"
				continue
				;;
			esac
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
			# The quick stage has no GitHub fork, so a submit it's told to
			# run can't be tried there (the M1's rerun, D-S6).
			if [ "$status" = 1 ] && [ "$verb" = submit ] && [ "${ACCEPT_STAGE:-}" = quick ] && tail -3 "$ROW_DIR/next.log" | grep -qE 'fork of|the quick stage (never pushes|reads the fork)'; then
				unverified="$unverified [dockhand $words: the stage has no GitHub fork]"
				continue
			fi
			case "$status" in 0 | 3) ;; *) broken="$broken [dockhand $words: exit $status]" ;; esac
		done <<EOT
$line
EOT
	done <<EOT
$(harm_next_lines)
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

# harm_next_lines are the row's Next: lines H6 runs: those after the last
# whole mark (next_superseded), less those naming a branch a later mark
# names (next_superseded_for), by its name or dockhand/ and its name.
harm_next_lines() {
	awk '
	/^# Next: lines above were superseded/ { n = 0; next }
	/^# Next: lines naming [^ ]+ above were superseded/ {
		b = $5; m = 0
		for (i = 1; i <= n; i++) {
			keep = 1
			c = split(lines[i], w, " ")
			for (k = 1; k <= c; k++) {
				t = w[k]
				gsub(/["\047,;)(]/, "", t)
				if (t == b || t == "dockhand/" b) keep = 0
			}
			if (keep) lines[++m] = lines[i]
		}
		n = m
		next
	}
	{ lines[++n] = $0 }
	END { for (i = 1; i <= n; i++) print lines[i] }
	' "$ROW_DIR/out.log" 2>/dev/null | grep -h '^Next: '
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
	# After the row's teardown, nothing it started is left, allowed or not.
	if [ -f "$after/running.teardown" ]; then
		while IFS= read -r line; do
			[ -n "$line" ] || continue
			grep -qxF "$line" "$before/running" && continue
			case "$broken" in *"[$line]"*) continue ;; esac
			broken="$broken [$line, past teardown]"
		done <"$after/running.teardown"
	fi
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

# H9: the quick stage leaves the person's own state alone. Their
# ~/.dockhand, ~/.tart and ~/.ssh are as they were: none made, none gone,
# no file in them added, removed, or changed.
harm_h9() {
	local before=$1 after=$2 changed
	if [ -z "${ACCEPT_HOME_DIRS:-}" ]; then
		harm_write H9 "skipped: the stage's user is the test's own"
		return
	fi
	changed=$(diff "$before/home" "$after/home" | sed -n 's/^[<>] //p' | awk '{print $2}' | sort -u | head -5 | tr '\n' ' ')
	if [ -n "$changed" ]; then
		harm_write H9 "broken: changed your own state: ${changed% }"
	else
		harm_write H9 ok
	fi
}
