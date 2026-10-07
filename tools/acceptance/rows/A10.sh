# stages: quick full
# A10: docs as tests. Every dockhand line of every sh example in the
# README and usage.md runs as written, in the order the text has them, in
# the stage's scratch checkout: a cd into a branch's worktree goes there,
# and a branch an example names with --branch is started first, as the
# text assumes it. submit, which would push, and check, which would build,
# run as their --plan. A line that can't run here, as one that makes a
# Tart image, installs serve, or names a project that isn't one, is listed
# as not run, with why. Neither outdated --mine nor update --outdated
# --mine runs: each asks GitHub of every port the maintainer has, about
# 320 of Herby's, from the account's hourly quota, which field testing
# shares (the M1's rerun, 2026-10-03); outdated runs on the stage's two
# ports instead, and B3 runs a batch on them. jq, the docs' example, is current at the
# stage's pin, so the stage's small Go port, which is due, stands in for
# it; a line naming one of jq's versions isn't run.

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
	for file in "$ACCEPT_REPO/README.md" "$ACCEPT_REPO/docs/usage.md"; do
		awk '/^```sh/ {f=1; next} /^```/ {f=0} f' "$file" |
			sed 's/[[:space:]]#.*$//; s/^[[:space:]]*//; s/[[:space:]]*$//' | grep -E '^(dockhand |cd "\$\(dockhand path )'
	done
}

act() {
	local line words verb why dir branch name port=${ACCEPT_GO_PORT:?}
	dir=$MACPORTS_TREE
	: >"$ROW_DIR/a10.notrun"
	: >"$ROW_DIR/a10.failed"
	while IFS= read -r line; do
		case "$line" in
		'cd "$(dockhand path '*)
			line=${line//jq-/$port-}
			name=${line#cd \"\$(dockhand path }
			name=${name%%)*}
			name=${name%…}
			# The row's own name for a branch the docs name (run_name).
			case "$name" in "$port-"*) ;; *) name=$(run_name "$name") ;; esac
			branch=$(git -C "$MACPORTS_TREE" branch --list "dockhand/$name*" --format='%(refname:short)' | head -1)
			if [ -n "$branch" ] && dir=$("$DH_BIN" path "$branch"); then
				printf '$ cd %s\n' "$dir" >>"$ROW_DIR/out.log"
			else
				printf '%s (no branch %s to go to)\n' "$line" "$name" >>"$ROW_DIR/a10.failed"
			fi
			continue
			;;
		esac
		words=${line#dockhand }
		words=$(printf '%s' " $words " | sed "s/ jq / $port /g; s/^ //; s/ \$//")
		verb=${words%% *}
		why=""
		case "$line" in
		*'…'* | *'<'* | *'$('* | *owner/project* | *'~/Downloads'*) why="names something to fill in" ;;
		"dockhand bump"*) why="goes on to a pull request, and has no --plan" ;;
		"dockhand serve"*) why="runs or installs serve, which the stage doesn't" ;;
		"dockhand setup tart"*) why="makes a Tart image" ;;
		"dockhand submit --passing"*) why="submits, and has no --plan" ;;
		*" jq "[0-9]*) why="names one of jq's versions, and jq is current at the stage's pin" ;;
		"dockhand submit"*) [ "${ACCEPT_STAGE:-}" = quick ] && why="submits to a GitHub fork, which the quick stage hasn't" ;;
		*"--on sequoia"* | *"--on tahoe"* | *"--on "[0-9]*) [ "${ACCEPT_STAGE:-}" = quick ] && why="builds on another release's Tart image, and the quick stage makes only this Mac's" ;;
		*"--outdated --mine"*) why="prepares every outdated port of the maintainer's; B3 runs a batch on two" ;;
		"dockhand outdated --mine"*) why="asks GitHub of each of the maintainer's ports, hundreds, from the account's hourly quota; outdated runs on the stage's two ports instead" ;;
		esac
		if [ -n "$why" ]; then
			printf '%s: %s\n' "$line" "$why" >>"$ROW_DIR/a10.notrun"
			continue
		fi
		case "$words" in *"--branch "*)
			name=${words#*--branch }
			name=${name%% *}
			# A name the docs give is the row's own for this run, since an
			# earlier run's archived record holds it (the rc7 full stage).
			words=${words/--branch $name/--branch $(run_name "$name")}
			name=$(run_name "$name")
			"$DH_BIN" path "$name" >/dev/null 2>&1 || dh start "$name" >/dev/null 2>&1 || :
			;;
		esac
		case " submit check " in *" $verb "*)
			case " $words " in *" --plan "*) ;; *) words="$words --plan" ;; esac ;;
		esac
		# A batch starts what it shows only with --yes, without a terminal,
		# as the docs' reader would answer its question.
		case " $words " in *" --outdated "*)
			case " $words " in *" -y "* | *" --yes "*) ;; *) words="$words -y" ;; esac ;;
		esac
		# shellcheck disable=SC2086
		eval "set -- $words"
		local before
		before=$(wc -c <"$ROW_DIR/out.log" | tr -d ' ')
		(cd "$dir" && dh "$@" </dev/null)
		case $? in
		0 | 3) ;;
		*)
			# A release whose Tart image the stage hasn't made is refused,
			# naming the setup that makes it, as it should be: the example
			# needs that image first, which isn't the docs' fault (the rc6
			# full stage).
			if [ "$verb" = submit ] && tail -c +"$((before + 1))" "$ROW_DIR/out.log" | grep -q "no check has finished for this commit's files"; then
				# submit's preview refuses a commit no check passed, as it
				# should, and A10 runs check only as --plan; rc5 passed only by
				# finding A4's checked branch (the rc6 full stage).
				printf '%s: needs a finished check, and A10 runs check only as its plan\n' "$line" >>"$ROW_DIR/a10.notrun"
			elif tail -c +"$((before + 1))" "$ROW_DIR/out.log" | grep -q 'no Tart image for macOS .*: dockhand setup tart'; then
				printf '%s: needs a Tart image the stage hasn'"'"'t made: %s\n' "$line" "$(tail -c +"$((before + 1))" "$ROW_DIR/out.log" | grep -o 'no Tart image for macOS [^:]*' | head -1)" >>"$ROW_DIR/a10.notrun"
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
		row_pass "every example ran; not run here: $(wc -l <"$ROW_DIR/a10.notrun" | tr -d ' ')"
	fi
}
