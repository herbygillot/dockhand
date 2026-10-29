# stages: full
# G21: serve stopped after a branch's commit and before its check
# (Codex's workflow review of 2ad896d2, W6). serve's daily look, over
# ACCEPT_SERVE_MAINTAINER's ports with ship_to = "check", is killed at the
# acceptance build's ship.committed failpoint, once it has committed a
# prepared branch and before it queues its check; the next serve says
# "serve: <branch> was prepared and never checked; check-N queued" and
# queues it. The row then stops that serve and cancels the check. Without
# a maintainer whose ports are few and some outdated, the row isn't run.
act() {
	host_only "serve's daily look over a few outdated ports" || return 0
	if [ -z "${ACCEPT_SERVE_MAINTAINER:-}" ]; then
		row_result "not run" "set ACCEPT_SERVE_MAINTAINER to a maintainer with a few ports, one or more outdated today"
		return 0
	fi
	allow_change "*"
	allow_ref_gone "*"
	cp "${DOCKHAND_CONFIG:?}" "$ROW_DIR/g21.toml"
	printf '\n[serve]\nship_to = "check"\noutdated_at = "00:00"\nmaintainers = ["%s"]\nnotify = false\n' "$ACCEPT_SERVE_MAINTAINER" >>"$ROW_DIR/g21.toml"
	# Today's look hasn't run, as far as this serve can tell.
	find "${DOCKHAND_DB%/*}" -path '*/serve/*/outdated.stamp' -exec rm -f {} + 2>/dev/null || :
	local config=$DOCKHAND_CONFIG
	export DOCKHAND_CONFIG="$ROW_DIR/g21.toml"
	printf '$ DOCKHAND_FAILPOINT=ship.committed:kill dockhand serve\n' >>"$ROW_DIR/out.log"
	DOCKHAND_FAILPOINT=ship.committed:kill with_timeout 1800 "${DH_FAILPOINT_BIN:?}" serve </dev/null >"$ROW_DIR/killed.log" 2>&1 || :
	cat "$ROW_DIR/killed.log" >>"$ROW_DIR/out.log"
	dh_bg serve
	wait_for_line "$DH_BG_LOG" "was prepared and never checked" 600 || :
	kill "$DH_BG_PID" 2>/dev/null
	dh_bg_wait || :
	export DOCKHAND_CONFIG="$config"
	G21_RUN=$(grep -oE 'was prepared and never checked; check-[0-9]+ queued' "$ROW_DIR/out.log" | grep -oE 'check-[0-9]+' | head -1)
	[ -z "$G21_RUN" ] || dh cancel "$G21_RUN" || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/killed.log" ] || return 0
	if ! matches -E 'committed|prepared|update' "$ROW_DIR/killed.log"; then
		row_result "not run" "serve's look prepared nothing for $ACCEPT_SERVE_MAINTAINER, so nothing was committed to stop at"
		return
	fi
	[ -n "${G21_RUN:-}" ] || { row_fail "the next serve didn't say a prepared branch was never checked and queue it: $(grep -E '^serve:' "$ROW_DIR/out.log" | tail -4 | tr '\n' ';')"; return; }
	row_pass "serve, stopped after committing a prepared branch, was followed by one that said the branch was never checked and queued $G21_RUN"
}
