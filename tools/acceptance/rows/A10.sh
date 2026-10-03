# stages: quick full
# A10: docs as tests. Every dockhand line of every sh example in the
# README and usage.md runs as written, in the order the text has them, in
# the stage's scratch checkout: a cd into a branch's worktree goes there,
# and a branch an example names with --branch is started first, as the
# text assumes it. submit, which would push, and check, which would build,
# run as their --plan. A line that can't run here, as one that makes a
# Tart image, installs serve, or names a project that isn't one, is listed
# as not run, with why. outdated --mine reads ACCEPT_MAINTAINER's ports.

setup() {
	local maintainer=${ACCEPT_MAINTAINER:-@herbygillot}
	printf 'maintainer = "%s"\n' "$maintainer" >"$ROW_DIR/maintainer.toml"
	# The maintainer line goes before any table, where a top-level key must.
	{ cat "$ROW_DIR/maintainer.toml"; cat "${DOCKHAND_CONFIG:?}"; } >"$ROW_DIR/config.toml" && cp "$ROW_DIR/config.toml" "$DOCKHAND_CONFIG"
}

a10_examples() {
	local file
	for file in "$ACCEPT_REPO/README.md" "$ACCEPT_REPO/docs/usage.md"; do
		awk '/^```sh/ {f=1; next} /^```/ {f=0} f' "$file" |
			sed 's/[[:space:]]#.*$//; s/^[[:space:]]*//; s/[[:space:]]*$//' | grep -E '^(dockhand |cd "\$\(dockhand path )'
	done
}

act() {
	local line words verb why dir branch name
	dir=$MACPORTS_TREE
	: >"$ROW_DIR/a10.notrun"
	: >"$ROW_DIR/a10.failed"
	while IFS= read -r line; do
		case "$line" in
		'cd "$(dockhand path '*)
			name=${line#cd \"\$(dockhand path }
			name=${name%%)*}
			name=${name%…}
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
		verb=${words%% *}
		why=""
		case "$line" in
		*'…'* | *'<'* | *'$('* | *owner/project* | *'~/Downloads'*) why="names something to fill in" ;;
		"dockhand bump"*) why="goes on to a pull request, and has no --plan" ;;
		"dockhand serve"*) why="runs or installs serve, which the stage doesn't" ;;
		"dockhand setup tart"*) why="makes a Tart image" ;;
		"dockhand submit --passing"*) why="submits, and has no --plan" ;;
		esac
		if [ -n "$why" ]; then
			printf '%s: %s\n' "$line" "$why" >>"$ROW_DIR/a10.notrun"
			continue
		fi
		case "$words" in *"--branch "*)
			name=${words#*--branch }
			name=${name%% *}
			"$DH_BIN" path "$name" >/dev/null 2>&1 || dh start "$name" >/dev/null 2>&1 || :
			;;
		esac
		case " submit check " in *" $verb "*)
			case " $words " in *" --plan "*) ;; *) words="$words --plan" ;; esac ;;
		esac
		# shellcheck disable=SC2086
		eval "set -- $words"
		(cd "$dir" && dh "$@" </dev/null)
		case $? in 0 | 3) ;; *) printf '%s (run as dockhand %s, in %s)\n' "$line" "$words" "$dir" >>"$ROW_DIR/a10.failed" ;; esac
	done <<EOT
$(a10_examples)
EOT
}

assert() {
	if [ -s "$ROW_DIR/a10.failed" ]; then
		row_fail "examples that didn't run as written: $(tr '\n' ';' <"$ROW_DIR/a10.failed")"
	else
		row_pass "every example ran; not run here: $(wc -l <"$ROW_DIR/a10.notrun" | tr -d ' ')"
	fi
}
