# shellcheck shell=bash
# guard refuses a run that could touch a person's own dockhand state (the
# project's plan/acceptance-harness.md, H2). The quick stage runs in the
# person's own account, so every path dockhand writes must be inside the
# stage's scratch directory, ACCEPT_STATE. The full stage runs only as the
# test host's throwaway user, dhtest, whose state is the test's.

# guard_resolve is a path with its deepest existing directory's links
# resolved, as macOS names /var by /private/var.
guard_resolve() {
	local path=$1 rest=""
	while [ ! -d "$path" ] && [ "$path" != / ]; do
		rest="/$(basename "$path")$rest"
		path=$(dirname "$path")
	done
	printf '%s%s' "$(cd "$path" && pwd -P)" "$rest"
}

# guard_inside says whether a path is inside a directory.
guard_inside() {
	case "$1/" in "$2"/*) return 0 ;; esac
	return 1
}

guard() {
	local stage=$1 state name value
	if [ "$stage" = full ]; then
		if [ "$(id -un)" != dhtest ]; then
			echo "guard: the full stage runs only as the test host's dhtest user, never in an account someone uses" >&2
			return 1
		fi
		# Test pull requests go to the sandbox; only those marked real
		# go to MacPorts, one call at a time.
		case "${DOCKHAND_PULL_REQUESTS:-}" in
		"" | [Mm]ac[Pp]orts/macports-ports)
			echo "guard: DOCKHAND_PULL_REQUESTS must name the test account's sandbox, so test pull requests stay out of MacPorts; full.sh sets it" >&2
			return 1
			;;
		esac
		return 0
	fi
	state=$(cd "${ACCEPT_STATE:?}" && pwd -P)
	for name in DOCKHAND_DB DOCKHAND_CONFIG MACPORTS_TREE DOCKHAND_UPSTREAM DOCKHAND_INDEX_CACHE DOCKHAND_READING_CACHE DOCKHAND_TART_HOME DOCKHAND_SSH_DIR TART_HOME; do
		value=$(eval "printf '%s' \"\${$name:-}\"")
		if [ -z "$value" ]; then
			echo "guard: $name is unset, so dockhand would use your own; the quick stage sets every one inside $state" >&2
			return 1
		fi
		case "$value" in /*) ;; *) value="$PWD/$value" ;; esac
		value=$(guard_resolve "$value")
		if ! guard_inside "$value" "$state"; then
			echo "guard: $name is $value, outside the stage's $state; nothing was run" >&2
			return 1
		fi
	done
	for value in "$HOME/.dockhand/dockhand.db" "$HOME/.dockhand/config.toml"; do
		if [ "${DOCKHAND_DB:-}" = "$value" ] || [ "${DOCKHAND_CONFIG:-}" = "$value" ]; then
			echo "guard: $value is your own; nothing was run" >&2
			return 1
		fi
	done
}
