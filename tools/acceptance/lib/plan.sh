# shellcheck shell=bash
# Helpers for rows that judge an update by its plan: run it, and read
# what it said.

# plan_update runs update <port> --new --plan as a person reads it, for
# the words a row judges, and again with --json, for the result and H8,
# and says the envelope's file.
plan_update() {
	dh update "$@" --new --plan >/dev/null 2>&1 || :
	dh_json update "$@" --new --plan || :
	printf '%s' "$DH_LAST_JSON"
}

# plan_ok says whether an update's plan worked, or was refused with a
# reason: a correct result or a clean refusal, never a crash.
plan_ok() {
	local file=$1 status
	status=$(cat "$file.exit")
	case "$status" in
	0 | 3) return 0 ;;
	1) [ -n "$(jq -r '.error // ""' "$file")" ] && ! grep -q '^panic:' "$ROW_DIR/out.log" ;;
	*) return 1 ;;
	esac
}

# plan_words is what the update's plan said to a person, from its log.
plan_words() { sed -n "/^\\\$ dockhand update $1 /,/^\\[exit/p" "$ROW_DIR/out.log"; }

# plan_current says whether an update's plan found its port current at
# the stage's master, "is already at …; nothing to change": the row's
# fixture is no longer due, as git-credential-oauth wasn't once MacPorts
# had 0.18.0 (44bbe1c, the M1's rc2 full stage, whose C11 then failed).
plan_current() { plan_words "$1" | matches -E ' is already at [^;]*; nothing to change'; }

# plan_not_due says a row isn't run, its fixture port current at the
# stage's master, as dh_setup says an update --new's is (B4).
plan_not_due() {
	row_result "not run" "its fixture port, $1, is current at the stage's master, so its plan had nothing to change; name a port that's due"
}

# plan_at_pin gives the row a master of its own at the quick stage's pin,
# a bare repository borrowing the ports clone's objects, as quick.sh makes,
# named by DOCKHAND_UPSTREAM for the row alone. The full stage's master is
# MacPorts' own, where a fixture port has since moved on, so its update had
# nothing to change and the case never ran (C4 at the rc6 full stage; C11
# and C13 at rc4's). The quick stage's master is the pin already.
plan_at_pin() {
	[ "${ACCEPT_STAGE:-}" = full ] || return 0
	local pin=${ACCEPT_PIN:-026b3878a258b85619633709d89871f2f8750b94} objects
	if ! git -C "${MACPORTS_TREE:?}" cat-file -e "$pin^{commit}" 2>/dev/null; then
		row_result "not run" "the ports clone hasn't the quick stage's pin, $pin, where $1"
		return 0
	fi
	objects=$(git -C "$MACPORTS_TREE" rev-parse --path-format=absolute --git-common-dir)/objects
	git init -q --bare -b master "$ROW_DIR/upstream.git" || return 1
	printf '%s\n' "$objects" >"$ROW_DIR/upstream.git/objects/info/alternates"
	git -C "$ROW_DIR/upstream.git" update-ref refs/heads/master "$pin" || return 1
	export DOCKHAND_UPSTREAM="$ROW_DIR/upstream.git"
}
