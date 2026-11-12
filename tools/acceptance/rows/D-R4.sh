# stages: full
# D-R4: Tart, or MacPorts, gone after setup, hidden by a PATH shim: refused
# with what's missing; nothing else breaks.
. "$ROW_LIB/fault.sh"
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }
# Its check names a branch of its own, and its update plans from master:
# with earlier rows' branches set aside, both were refused for want of a
# branch before they missed Tart or MacPorts (the rc6 full stage).
setup() {
	host_only "a set-up Tart and MacPorts" || return 0
	dh_setup update "$(port)" --new || return 1
	DR4_BRANCH=$(own_branch)
}
# dr4_path is PATH with every directory that holds a MacPorts program
# taken out, so port, port-tclsh and portindex are really missing: a shim
# of port alone left port-tclsh and portindex working (Codex's cold read
# at 2cdaf571, #232).
dr4_path() {
	local dir out=""
	local IFS=:
	for dir in $PATH; do
		[ -x "$dir/port" ] || [ -x "$dir/port-tclsh" ] || [ -x "$dir/portindex" ] && continue
		out="${out:+$out:}$dir"
	done
	printf '%s' "$out"
}
act() {
	host_only "a set-up Tart and MacPorts" || return 0
	local shim from path exited
	shim=$(fault_shims tart)
	# --fresh, so a reused build doesn't skip the tart the shim is.
	from=$(log_size "$ROW_DIR/out.log")
	exited=0
	PATH="$shim:$PATH" "$DH_BIN" check -b "$DR4_BRANCH" --fresh >>"$ROW_DIR/out.log" 2>&1 || exited=$?
	echo "[exit $exited]" >>"$ROW_DIR/out.log"
	tail -c +"$((from + 1))" "$ROW_DIR/out.log" >"$ROW_DIR/tart.out"
	echo "$exited" >"$ROW_DIR/tart.exit"
	rm -rf "$shim"
	path=$(dr4_path)
	# Proven missing before it's judged: none of MacPorts' programs on the
	# PATH the command gets, and none named by the variables dockhand reads.
	if PATH="$path" command -v port port-tclsh portindex >/dev/null 2>&1; then
		echo "still found" >"$ROW_DIR/macports.unmissing"
	fi
	from=$(log_size "$ROW_DIR/out.log")
	exited=0
	env -u PORT_TCLSH_BIN -u PORTINDEX_BIN PATH="$path" "$DH_BIN" update "$(port)" --new --plan >>"$ROW_DIR/out.log" 2>&1 || exited=$?
	echo "[exit $exited]" >>"$ROW_DIR/out.log"
	tail -c +"$((from + 1))" "$ROW_DIR/out.log" >"$ROW_DIR/macports.out"
	echo "$exited" >"$ROW_DIR/macports.exit"
	dh_json status || :
}
assert() {
	[ ! -f "$ROW_DIR/macports.unmissing" ] || { row_result "not exercised" "MacPorts' programs were still found on the PATH meant to hide them"; return; }
	# Each command's own output, by where it began in the log, and its own
	# exit: refused, naming what's missing.
	[ "$(cat "$ROW_DIR/tart.exit")" != 0 ] && grep -qi 'tart' "$ROW_DIR/tart.out" ||
		{ row_fail "with Tart hidden, the check didn't refuse naming Tart: exit $(cat "$ROW_DIR/tart.exit"), $(tail -2 "$ROW_DIR/tart.out" | tr '\n' ' ')"; return; }
	[ "$(cat "$ROW_DIR/macports.exit")" != 0 ] && grep -qiE 'macports|port-tclsh|portindex' "$ROW_DIR/macports.out" ||
		{ row_fail "with MacPorts missing, the update didn't refuse naming it: exit $(cat "$ROW_DIR/macports.exit"), $(tail -2 "$ROW_DIR/macports.out" | tr '\n' ' ')"; return; }
	[ "$(cat "$ROW_DIR/json/1.json.exit")" = 0 ] && row_pass "each refused naming what's missing, and status still works" || row_fail "status broke"
}
