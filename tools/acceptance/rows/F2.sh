# stages: full
# F2: a shallow clone (--depth 1) and a partial clone (--filter=blob:none)
# of the ports tree: setup works, or refuses well and names the fetch that
# fixes it, and an update plans in each.
#
# setup has no --json, and outdated --mine needs a maintainer, so the row
# read nothing (the rc6 full stage): it runs plain setup, reading its exit
# and its lines, and plans an update of the stage's Go port.
act() {
	host_only "a network clone of the ports tree" || return 0
	local how dir name
	for how in "--depth 1" "--filter=blob:none"; do
		name=${how//[^a-z0-9]/}
		dir=$ROW_DIR/clone-$name
		# shellcheck disable=SC2086
		git clone -q $how https://github.com/macports/macports-ports.git "$dir" || { echo "clone failed" >"$ROW_DIR/setup-$name.exit"; continue; }
		(cd "$dir" && MACPORTS_TREE=$dir "$DH_BIN" setup -y </dev/null >"$ROW_DIR/setup-$name.log" 2>&1)
		echo "$?" >"$ROW_DIR/setup-$name.exit"
		(cd "$dir" && MACPORTS_TREE=$dir "$DH_BIN" update "${ACCEPT_GO_PORT:?}" --new --plan </dev/null >"$ROW_DIR/plan-$name.log" 2>&1)
		echo "$?" >"$ROW_DIR/plan-$name.exit"
		cat "$ROW_DIR/setup-$name.log" "$ROW_DIR/plan-$name.log" >>"$ROW_DIR/out.log"
		rm -rf "$dir"
	done
}
assert() {
	local name bad=""
	for name in depth1 filterblobnone; do
		case "$(cat "$ROW_DIR/setup-$name.exit" 2>/dev/null)" in
		0) grep -q '✗' "$ROW_DIR/setup-$name.log" && bad="$bad $name: setup said ✗;" ;;
		"clone failed") bad="$bad $name: the clone failed;" ;;
		*) grep -qiE 'fetch|unshallow|--filter|clone' "$ROW_DIR/setup-$name.log" || bad="$bad $name: setup failed without naming the fetch;" ;;
		esac
		case "$(cat "$ROW_DIR/plan-$name.exit" 2>/dev/null)" in
		0 | 3) ;;
		*) bad="$bad $name: update --plan exited $(cat "$ROW_DIR/plan-$name.exit" 2>/dev/null);" ;;
		esac
	done
	if [ -n "$bad" ]; then
		row_fail "$bad"
	else
		row_pass "setup and an update's plan worked in the shallow and the partial clone"
	fi
}
