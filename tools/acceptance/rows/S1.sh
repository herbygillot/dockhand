# stages: full
# order: alone
# S1: serve left running for a day on the real workload, with its daily
# look and cleanup: memory flat, disk held by cleanup, logs compressed,
# no orphaned VM, and the login renewed without a prompt. It runs alone,
# after the rest of the full run (full.sh --rows S1): a stage's run
# leaves it out, so it holds up nothing (the rc3 full run, 2026-10-06).
#
# A fresh account's serve does nothing to soak: no maintainer, so no
# look finds anything, and cleanup's defaults never act on a host with
# room. The row gives serve the person's ports to look at and check,
# and a short cleanup age, and samples serve's memory, the disk, and the
# logs every hour. The person chose a day (2026-10-06); ACCEPT_S1_HOURS
# changes it. It installs the one serve agent the account has, as B7 does,
# so the two can't run at once: B7's install would replace S1's agent, and
# its teardown remove it.
# fault_tart: tart in dockhand's Tart home, where its VMs are; a plain
# tart list read Tart's own home and came out empty (rc11's D-I5).
. "${ROW_LIB:?}/fault.sh"
hours() { printf '%s' "${ACCEPT_S1_HOURS:-24}"; }

setup() {
	local maintainer=${ACCEPT_MAINTAINER:-@herbygillot}
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/config.before"
	# Top-level keys before any table; the row's tables after the rest,
	# which a fresh account's config has none of.
	{
		printf 'maintainer = "%s"\n' "$maintainer"
		sed '/^maintainer *=/d' "$DOCKHAND_CONFIG"
		printf '\n[serve]\nfor_outdated = "check"\n\n[cleanup]\nafter = "12h"\n'
	} >"$ROW_DIR/config.toml"
	if grep -qE '^\[(serve|cleanup)\]' "$ROW_DIR/config.before"; then
		echo "S1: $DOCKHAND_CONFIG already has a [serve] or [cleanup] table, which the row's would repeat" >&2
		return 1
	fi
	cp "$ROW_DIR/config.toml" "$DOCKHAND_CONFIG"
}

# sample writes one line of what the soak watches: the time, serve's
# memory in kilobytes, the free disk, and the logs, compressed and not.
sample() {
	local logs=$HOME/.dockhand/logs rss pid
	# serve's own pid, as it records it: a pgrep for "dockhand serve"
	# caught serve --install's, 384 KB (rc11's soak).
	pid=$(s1_pid)
	[ -n "$pid" ] && rss=$(ps -o rss= -p "$pid" 2>/dev/null | tr -d ' ')
	printf '%s rss_kb=%s free_kb=%s logs=%s gz=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${rss:-none}" \
		"$(df -k "$HOME" | awk 'NR == 2 {print $4}')" \
		"$(find "$logs" -type f ! -name '*.gz' 2>/dev/null | wc -l | tr -d ' ')" \
		"$(find "$logs" -type f -name '*.gz' 2>/dev/null | wc -l | tr -d ' ')"
}

# s1_pid is the pid of the serve that leads, as queue reports it.
s1_pid() {
	dh_quiet --json queue | jq -r '.result.serve_state.pid // empty' 2>/dev/null
}

act() {
	host_only "a day of serve on the host" || return 0
	allow_change "*"
	dh serve --install || return 0
	local i waited
	# The first sample is serve's, once it has started under launchd.
	for ((waited = 0; waited < 120; waited += 5)); do
		[ -n "$(s1_pid)" ] && break
		sleep 5
	done
	for ((i = 0; i <= $(hours); i++)); do
		[ "$i" = 0 ] || sleep 3600
		sample >>"$ROW_DIR/samples"
		# Straight to the row's file, a line each with when: dh writes
		# to out.log, which left this file empty (rc11's soak).
		printf '%s ' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$ROW_DIR/auth"
		"$DH_BIN" auth status >>"$ROW_DIR/auth" 2>&1 || printf '[exit %d]\n' "$?" >>"$ROW_DIR/auth"
	done
	fault_tart list 2>/dev/null >"$ROW_DIR/vms" || :
	dh serve --uninstall || :
}

teardown() {
	[ -f "$ROW_DIR/config.before" ] && cp "$ROW_DIR/config.before" "$DOCKHAND_CONFIG"
	return 0
}

assert() {
	judged "over $(hours) hours, from $ROW_DIR/samples, auth, and vms: serve's memory stayed flat, cleanup held the disk, logs were compressed, no VM was orphaned, and the login stayed good without a prompt"
}
