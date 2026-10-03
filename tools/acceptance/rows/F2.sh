# stages: full
# F2: a shallow clone (--depth 1) and a partial clone (--filter=blob:none)
# of the ports tree: works, or setup refuses well and names the fetch
# that fixes it.
act() {
	host_only "a network clone of the ports tree" || return 0
	local how dir
	for how in "--depth 1" "--filter=blob:none"; do
		dir=$ROW_DIR/clone-${how//[^a-z0-9]/}
		# shellcheck disable=SC2086
		git clone -q $how https://github.com/macports/macports-ports.git "$dir" || continue
		(cd "$dir" && MACPORTS_TREE=$dir "$DH_BIN" --json setup -y >"$ROW_DIR/setup-${how//[^a-z0-9]/}.json" 2>>"$ROW_DIR/out.log") || :
		(cd "$dir" && MACPORTS_TREE=$dir "$DH_BIN" outdated --mine >>"$ROW_DIR/out.log" 2>&1) || :
		rm -rf "$dir"
	done
}
assert() {
	judged "each clone worked, or setup refused naming the fetch that fixes it"
}
