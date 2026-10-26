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
