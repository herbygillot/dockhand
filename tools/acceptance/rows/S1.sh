# stages: full
# S1: serve left running for 7 days, with its daily look and cleanup:
# memory flat, disk held by cleanup, logs compressed, no orphaned VM, and
# the login renewed every day without a prompt. It runs last.
act() {
	host_only "a week of serve on the host" || return 0
	allow_change "*"
	dh serve --install || return 0
	df -k "$HOME" | tail -1 >"$ROW_DIR/disk.start"
	checkpoint "leave serve running for 7 days, noting its memory (ps -o rss) each day in $ROW_DIR/notes" || return 0
	df -k "$HOME" | tail -1 >"$ROW_DIR/disk.end"
	dh auth status || :
	dh serve --uninstall || :
}
assert() {
	judged "memory stayed flat, cleanup held the disk, logs were compressed, no VM was orphaned, and the login renewed daily"
}
