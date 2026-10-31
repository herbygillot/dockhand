# stages: quick full
# A10: docs as tests. Every dockhand line of every sh example in the
# README and the usage pages runs as written, in the order the text has them, in
# the stage's scratch checkout: a cd into a branch's worktree goes there,
# and a branch an example names with --branch is started first, as the
# text assumes it. submit, which would push, and check, which would build,
# run as their --plan. A line that can't run here, as one that makes a
# Tart image, installs serve, or names a project that isn't one, is listed
# as not run, with why. ship runs as its --plan too. None of outdated
# --mine, update --mine, and ship --mine runs: each asks GitHub of every port the maintainer has, about
# 320 of Herby's, from the account's hourly quota, which field testing
# shares (the M1's rerun, 2026-10-03); outdated runs on the stage's two
# ports instead, and B3 runs a batch on them. jq and ripgrep, the docs'
# examples, are current at the stage's pin, so the stage's small Go port,
# which is due, stands in for each; a line naming one of jq's versions
# isn't run. setup github isn't run: its device login opened a browser on
# the M1 and waited 15 minutes for the code (the rc5 rerun, 2026-10-19).

setup() {
	local maintainer=${ACCEPT_MAINTAINER:-@herbygillot}
	printf 'maintainer = "%s"\n' "$maintainer" >"$ROW_DIR/maintainer.toml"
	# The maintainer line goes before any table, where a top-level key must.
	# Kept to restore in teardown: in the full stage it's dhtest's own,
	# which outlasts the row; a maintainer line already there is replaced.
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/config.before"
	# sed, not grep -v, which exits 1 for a config with no other line, as
	# a fresh account's empty one (the rc3 full run, 2026-10-06).
	{ cat "$ROW_DIR/maintainer.toml"; sed '/^maintainer *=/d' "$DOCKHAND_CONFIG"; } >"$ROW_DIR/config.toml" && cp "$ROW_DIR/config.toml" "$DOCKHAND_CONFIG"
}

teardown() {
	[ -f "$ROW_DIR/config.before" ] && cp "$ROW_DIR/config.before" "$DOCKHAND_CONFIG"
	return 0
}

a10_examples() {
	local file
	for file in "$ACCEPT_REPO/README.md" \
		"$ACCEPT_REPO"/docs/usage/setup.md "$ACCEPT_REPO"/docs/usage/branches.md "$ACCEPT_REPO"/docs/usage/hand-edits.md "$ACCEPT_REPO"/docs/usage/checks.md \
		"$ACCEPT_REPO"/docs/usage/publishing.md "$ACCEPT_REPO"/docs/usage/recovery.md "$ACCEPT_REPO"/docs/usage/batches.md "$ACCEPT_REPO"/docs/usage/unattended.md \
		"$ACCEPT_REPO"/docs/usage/new-port.md "$ACCEPT_REPO"/docs/usage/review.md "$ACCEPT_REPO"/docs/usage/agents.md "$ACCEPT_REPO"/docs/usage/configuration.md; do
		printf '# page %s\n' "$file"
		awk '/^```sh/ {f=1; next} /^```/ {f=0} f' "$file" |
			sed 's/[[:space:]]#.*$//; s/^[[:space:]]*//; s/[[:space:]]*$//' | grep -E '^(dockhand |cd "\$\(dockhand path )'
	done
}

# a10_name is the row's own name for a branch the docs name: jq's and
# ripgrep's the stage's port's, then stamped for this run (run_name), the
# same wherever the docs name it, a cd, -b, --branch, --new=, or start's.
# The docs' jq-update was run as jq-update-<stamp> by -b and jlv-update by
# cd, so neither found the other's (rc1's quick stage).
a10_name() {
	local name=$1 port=${ACCEPT_GO_PORT:?}
	case "$name" in jq-* | ripgrep-*) name=$port-${name#*-} ;; esac
	case "$name" in "$port-"*"-$(cat "$ROW_DIR/run-stamp" 2>/dev/null)") printf '%s' "$name" ;; *) run_name "$name" ;; esac
}

# a10_branch is the newest Git branch dockhand/<name>*, preferring one
# that wasn't open before the row (branches.before), whose names are
# dockhand's, with or without the dockhand/ prefix.
a10_branch() {
	local refs mine
	refs=$(git -C "$MACPORTS_TREE" branch --list "dockhand/$1*" --sort=-creatordate --format='%(refname:short)')
	mine=$(printf '%s\n' "$refs" | while IFS= read -r ref; do
		[ -n "$ref" ] || continue
		grep -qxF -e "$ref" -e "${ref#dockhand/}" "$ROW_DIR/branches.before" 2>/dev/null || printf '%s\n' "$ref"
	done | head -1)
	printf '%s\n' "${mine:-$(printf '%s\n' "$refs" | head -1)}"
}

act() {
	local line words verb why dir made branch name named port=${ACCEPT_GO_PORT:?}
	dir=$MACPORTS_TREE made=$MACPORTS_TREE
	: >"$ROW_DIR/a10.notrun"
	: >"$ROW_DIR/a10.failed"
	: >"$ROW_DIR/a10.refused"
	while IFS= read -r line; do
		case "$line" in
		# A cd not made stops the lines after it on its page alone; the
		# next page starts where the last cd made went.
		'# page '*)
			dir=$made
			continue
			;;
		'cd "$(dockhand path '*)
			name=${line#cd \"\$(dockhand path }
			name=${name%%)*}
			name=${name%…}
			# A cd into a branch the docs name for the reader to fill in,
			# or one a step not run here would have made, isn't made, and
			# the lines after it, which run there, aren't run either: they
			# ran in the branch before, and failed as its (rc1's quick
			# stage, BRANCH and mytool).
			case "$name" in
			BRANCH | mytool | pr-*)
				printf '%s: goes to a branch the docs name for you to fill in, or one a step not run here makes\n' "$line" >>"$ROW_DIR/a10.notrun"
				dir=""
				continue
				;;
			"-p "*)
				# The row's newest branch of the port, which path -p asks
				# about where earlier examples left several.
				branch=$(a10_branch "${ACCEPT_GO_PORT:?}")
				;;
			*)
				# The row's own name first, as update --new=<name> made it:
				# an earlier row's branch of the same name, A4's
				# dblab-update, sorted first by its newer commit, and submit
				# refused its closed pull request (the rc4 full stage).
				branch=$(a10_name "$name")
				"$DH_BIN" path "$branch" >/dev/null 2>&1 || branch=$(a10_branch "${branch%-*}")
				;;
			esac
			if [ -n "$branch" ] && dir=$("$DH_BIN" path "${branch#dockhand/}"); then
				printf '$ cd %s\n' "$dir" >>"$ROW_DIR/out.log"
				made=$dir
			else
				printf '%s (no branch %s to go to)\n' "$line" "$name" >>"$ROW_DIR/a10.failed"
				dir=""
			fi
			continue
			;;
		esac
		words=${line#dockhand }
		# A commit message the docs give names jq, which the stage's port's
		# commit rules refuse, so tidy asked again (rc1's quick stage).
		words=$(printf '%s' " $words " | sed "s/ jq / $port /g; s/ ripgrep / $port /g; s/ --new=ripgrep-/ --new=$port-/g; s/\"jq: /\"$port: /g; s/^ //; s/ \$//")
		verb=${words%% *}
		why=""
		case "$line" in
		*'…'* | *'<'* | *'$('* | *owner/project* | *'~/Downloads'* | *OWNER/* | *NAME* | *MODULE* | *PROGRAM* | *BRANCH* | *DEPENDENT*) why="names something to fill in" ;;
		*'\') why="goes on over the next lines" ;;
		*'>'*) why="writes a file of its own" ;;
		# The guide's examples of acting on someone else's pull request
		# name a real one: none runs, so the stage never posts on it or
		# adopts it (Codex's pages, 2026-10-28).
		"dockhand review "*"--comment"* | "dockhand review "*"--request-changes"*) why="posts a review on a real pull request" ;;
		"dockhand review "*"--check"* | "dockhand adopt --pr"*) why="adopts a real pull request into the stage" ;;
		"dockhand open"*) why="opens a browser" ;;
		*check-[0-9]* | "dockhand wait"*) why="names or follows a check, and A10 runs check only as its plan" ;;
		"dockhand setup --worktrees"*) why="moves the stage's worktrees" ;;
		"dockhand undo tidy-"* | "dockhand start --from"*) why="names a checkpoint or a merged branch the stage hasn't" ;;
		"dockhand clean") why="asks first, and refuses without a terminal; clean --plan runs" ;;
		*"--hand-edit"*) why="writes a hand edit only where update refuses, and the stage's port updates" ;;
		*kubectl* | *libharbor* | *mytool* | "dockhand move dockhand "*) why="names a port the guide uses as an example" ;;
		*"--variants '"*) why="names jq's variants, which the stage's port hasn't" ;;
		*"--pr "[0-9]*) why="follows adopt --pr, which isn't run" ;;
		"dockhand outdated --maintainer "* | "dockhand outdated --category "*) why="asks GitHub of thousands of ports, past the account's hourly allowance" ;;
		# The guide's rename is jq's to a name of its own; its plan runs
		# on the stage's port, and the rename itself, which would move the
		# port the later examples edit, is G6's (rc1's full stage: the
		# guide's goplus became xgo on master, and the plan refused).
		"dockhand rename "*" --plan") ;;
		"dockhand rename "* | *jqlang*) why="renames the port the later examples edit; G6 runs a rename whole" ;;
		"dockhand bump"*) why="goes on to a pull request, and has no --plan" ;;
		"dockhand serve"*) why="runs or installs serve, which the stage doesn't" ;;
		"dockhand setup tart"*) why="makes a Tart image" ;;
		"dockhand setup github"*) why="starts GitHub's device login, which opens a browser and waits for its code; the stage signs in on its own" ;;
		*" jq "[0-9]*) why="names one of jq's versions, and jq is current at the stage's pin" ;;
		"dockhand submit"*) [ "${ACCEPT_STAGE:-}" = quick ] && why="submits to a GitHub fork, which the quick stage hasn't" ;;
		*"--on sequoia"* | *"--on tahoe"* | *"--on "[0-9]* | *"--on tart:"[a-z0-9]*) [ "${ACCEPT_STAGE:-}" = quick ] && why="builds on another release's Tart image, and the quick stage makes only this Mac's" ;;
		*"update --mine"* | *"ship --mine"*) why="prepares every outdated port of the maintainer's; B3 runs a batch on two" ;;
		"dockhand outdated --mine"*) why="asks GitHub of each of the maintainer's ports, hundreds, from the account's hourly quota; outdated runs on the stage's two ports instead" ;;
		esac
		[ -n "$why" ] || [ -n "$dir" ] || why="runs in a branch whose cd wasn't made"
		if [ -n "$why" ]; then
			printf '%s: %s\n' "$line" "$why" >>"$ROW_DIR/a10.notrun"
			continue
		fi
		# A name the docs give is the row's own for this run, since an
		# earlier run's archived record holds it (the rc7 full stage): -b and
		# --branch name a branch, started first as the text assumes, and
		# --new=<name> one to start (the rc1 full stage: ship -b jq-1.8.1,
		# --new=poppler-25.09).
		named=""
		for flag in --branch -b; do
			case " $words " in *" $flag "*)
				name=" $words "
				name=${name#* $flag }
				name=${name%% *}
				words=$(printf '%s' " $words " | sed "s| $flag $name | $flag $(a10_name "$name") |; s/^ //; s/ \$//")
				name=$(a10_name "$name")
				"$DH_BIN" path "$name" >/dev/null 2>&1 || dh start "$name" >/dev/null 2>&1 || :
				named=1
				;;
			esac
		done
		# ship <port> with no branch named takes the row's newest branch for
		# the port: the earlier update --new example started a second
		# go-reflex branch, and ship found two (the rc4 batch).
		case " $words " in " ship $port "*)
			case " $words " in *" -b "* | *" -p "* | *" --pr "* | *" --branch "*) ;;
			*)
				branch=$(a10_branch "$port")
				[ -z "$branch" ] || words=$(printf '%s' " $words " | sed "s| ship $port | ship -b ${branch#dockhand/} |; s/^ //; s/ \$//")
				;;
			esac ;;
		esac
		case "$words" in *"--new="*)
			name=${words#*--new=}
			name=${name%% *}
			words=${words/--new=$name/--new=$(a10_name "$name")}
			# An earlier example started it: the docs show two ways to
			# start the one branch.
			if "$DH_BIN" path "$(a10_name "$name")" >/dev/null 2>&1; then
				printf '%s: starts %s, which an earlier example started\n' "$line" "$name" >>"$ROW_DIR/a10.notrun"
				continue
			fi
			;;
		esac
		# path's, start's, and adopt's names are the row's own; adopt's is a Git
		# branch of the person's, made first, as the text assumes.
		case "$words" in
		"path "[a-z]*)
			set -- $words
			words="path $(a10_name "$2")"
			;;
		"start "*)
			set -- $words
			words=$(printf '%s' " $words " | sed "s| start $2 | start $(a10_name "$2") |; s/^ //; s/ \$//")
			;;
		"adopt "[a-z]*)
			set -- $words
			git -C "$MACPORTS_TREE" branch "$(a10_name "$2")" master >>"$ROW_DIR/out.log" 2>&1 || :
			words=$(printf '%s' " $words " | sed "s| adopt $2 | adopt $(a10_name "$2") |; s/^ //; s/ \$//")
			;;
		esac
		case " submit check ship " in *" $verb "*)
			case " $words " in *" --plan "*) ;; *) words="$words --plan" ;; esac
			# --plan and --yes go apart: the plan is what --yes would do.
			words=$(printf '%s' " $words " | sed 's/ --yes / /; s/^ //; s/ $//') ;;
		esac
		# shellcheck disable=SC2086
		eval "set -- $words"
		local before here=$dir
		before=$(wc -c <"$ROW_DIR/out.log" | tr -d ' ')
		# A step naming its branch runs from the clone, not the branch an
		# earlier cd went to.
		[ -z "$named" ] || here=$MACPORTS_TREE
		# The guide's rename plan runs in a branch of its own, clean: in
		# the branch an earlier cd went to, the examples before it left the
		# port's edits uncommitted, and rename rightly refused (rc2's quick
		# stage, rename jlv jqlang).
		case "$words" in "rename "*)
			here=$MACPORTS_TREE
			words="$words --new=$(a10_name jq-rename)"
			eval "set -- $words"
			;;
		esac
		# --apply reads the file --out wrote, from where -b's line ran.
		case " $words " in *" --apply "*)
			here=$MACPORTS_TREE
			name=" $words "
			name=${name#* --apply }
			name=${name%% *}
			# tidy --out writes no plan where there's nothing to tidy.
			if [ ! -f "$here/$name" ]; then
				printf '%s: applies a plan the --out before it didn'"'"'t write, with nothing to tidy\n' "$line" >>"$ROW_DIR/a10.notrun"
				continue
			fi
			;;
		esac
		case "$verb" in review)
			# As E9: the quick stage's pinned upstream has no pull
			# requests' refs, and the pull request is MacPorts'.
			if [ "${ACCEPT_STAGE:-}" = quick ] && [ -n "${DOCKHAND_UPSTREAM:-}" ]; then
				git -C "$DOCKHAND_UPSTREAM" fetch -q https://github.com/macports/macports-ports.git "refs/pull/$2/head:refs/pull/$2/head" >>"$ROW_DIR/out.log" 2>&1 || :
			fi
			;;
		esac
		(cd "$here" && if [ "$verb" = review ]; then unset DOCKHAND_PULL_REQUESTS; fi && dh "$@" </dev/null)
		case $? in
		0 | 3) ;;
		*)
			# A release whose Tart image the stage hasn't made is refused,
			# naming the setup that makes it, as it should be: the example
			# needs that image first, which isn't the docs' fault (the rc6
			# full stage).
			if [ "$verb" = submit ] && tail -c +"$((before + 1))" "$ROW_DIR/out.log" | matches "no check has finished for this commit's files"; then
				# submit's preview refuses a commit no check passed, as it
				# should, and A10 runs check only as --plan; rc5 passed only by
				# finding A4's checked branch (the rc6 full stage).
				printf '%s: needs a finished check, and A10 runs check only as its plan\n' "$line" >>"$ROW_DIR/a10.notrun"
			elif tail -c +"$((before + 1))" "$ROW_DIR/out.log" | matches 'has no finished check to compare with'; then
				printf '%s: needs a finished check, and A10 runs check only as its plan\n' "$line" >>"$ROW_DIR/a10.notrun"
			elif [ "$verb" = path ] && tail -c +"$((before + 1))" "$ROW_DIR/out.log" | matches 'is changed in more than one open branch'; then
				# Earlier examples left several of the port's branches open,
				# and path -p asks which, as it should.
				printf '%s: asks which of the branches earlier examples left open\n' "$line" >>"$ROW_DIR/a10.notrun"
			elif { [ "$verb" = logs ] || [ "$verb" = artifacts ]; } && tail -c +"$((before + 1))" "$ROW_DIR/out.log" | matches 'has no check yet'; then
				# logs in a worktree whose branch has no check says so, as it
				# should, and A10 runs check only as --plan (the rc5 rerun).
				printf '%s: needs a check, and A10 runs check only as its plan\n' "$line" >>"$ROW_DIR/a10.notrun"
			elif tail -c +"$((before + 1))" "$ROW_DIR/out.log" | matches 'dockhand setup tart'; then
				# A refusal naming the setup that makes the image ran as
				# written: the line is right, and this Mac lacks the image.
				# The grep wanted "…: dockhand setup tart" where the provider
				# says "…; run dockhand setup tart sequoia", and failed the
				# M1's --on sequoia and --on tahoe, refused rightly with only
				# macOS 27's images (the rc5 A10 rerun, 2026-10-20).
				printf '%s: refused, naming %s\n' "$line" "$(tail -c +"$((before + 1))" "$ROW_DIR/out.log" | grep -o 'dockhand setup tart[^ ;,]*\( [a-z][a-z-]*\)\{0,1\}' | head -1)" >>"$ROW_DIR/a10.refused"
			else
				printf '%s (run as dockhand %s, in %s)\n' "$line" "$words" "$dir" >>"$ROW_DIR/a10.failed"
			fi
			;;
		esac
	done <<EOT
$(a10_examples)
EOT
	a10_outdated
}

# a10_outdated is outdated --mine's stand-in: the stage's two ports.
a10_outdated() {
	(cd "$MACPORTS_TREE" && dh outdated "${ACCEPT_GO_PORT:?}" "${ACCEPT_RUST_PORT:?}" </dev/null)
	case $? in 0 | 3) ;; *) printf 'dockhand outdated %s %s (outdated --mine'"'"'s stand-in)\n' "$ACCEPT_GO_PORT" "$ACCEPT_RUST_PORT" >>"$ROW_DIR/a10.failed" ;; esac
}

assert() {
	if [ -s "$ROW_DIR/a10.failed" ]; then
		row_fail "examples that didn't run as written: $(tr '\n' ';' <"$ROW_DIR/a10.failed")"
	else
		row_pass "every example ran; refused for a Tart image this Mac lacks, as written: $(wc -l <"$ROW_DIR/a10.refused" | tr -d ' '); not run here: $(wc -l <"$ROW_DIR/a10.notrun" | tr -d ' ')"
	fi
}
