# shellcheck shell=bash
# What the local provider's rows share (v0.6.0, the G rows): a prefix's
# active ports as port lists them, and root for a check in /opt/local,
# whose registry is root's.

# local_active is the ports active in a prefix, each with its version,
# revision, and variants, sorted: what a check there must leave as it
# found, unless it keeps what it built.
local_active() {
	"$1/bin/port" -q installed active 2>/dev/null | awk '$3 == "(active)" { print $1, $2 }' | sort
}

# local_root has sudo's ticket ready for the row's checks in /opt/local,
# which ask for it once each, on this terminal: a person sitting at the
# M1 types dhtest's password, where the run has a terminal. A rule asking
# no password for /opt/local's port-tclsh, the one command a check runs
# as root (G8's), is as good. Without one, or without sudo, the row isn't
# run, saying why.
local_root() {
	if sudo -n /opt/local/bin/port-tclsh /dev/null 2>/dev/null || sudo -n /usr/bin/true 2>/dev/null; then
		return 0
	fi
	if [ ! -t 0 ] || ! sudo -v; then
		row_result "not run" "a check in /opt/local runs as root, and sudo asked for a password this run had no terminal for; run full.sh at dhtest's terminal, or give dhtest a sudo rule"
		return 1
	fi
}

# local_same fails the row unless a prefix's active ports are those
# before, saying what differs.
local_same() {
	local prefix=$1 before=$2 what=$3 now
	now=$(local_active "$prefix")
	if [ "$now" != "$(cat "$before")" ]; then
		row_fail "$what: $(diff "$before" <(printf '%s\n' "$now") | grep '^[<>]' | head -4 | tr '\n' ' ')"
		return 1
	fi
}
