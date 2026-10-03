# stages: quick full
# C9: kept archives and their signatures. A check keeps what it built, and
# each kept archive is signed once, beside it, not per check: after two
# checks of the small Go port, every archive has one signature per key.
# That a guest installs one as a dependency is the full stage's to see.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

c9_signatures() {
	find "$(dirname "${DOCKHAND_DB:?}")/archives" \( -name '*.sig' -o -name '*.rmd160' \) -exec stat -f '%m %N' {} + 2>/dev/null | sort -k2
}

act() {
	dh update "$(port)" --new || return 0
	dh_json check -p "$(port)" || :
	c9_signatures >"$ROW_DIR/c9.first"
	dh_json check -p "$(port)" --fresh || :
	c9_signatures >"$ROW_DIR/c9.second"
}

assert() {
	local archives sigs bad=""
	archives="$(dirname "${DOCKHAND_DB:?}")/archives"
	if [ ! -d "$archives" ] || [ -z "$(find "$archives" -type f ! -name '*.sig' ! -name '*.rmd160' | head -1)" ]; then
		row_fail "no archive was kept in $archives"
		return
	fi
	while IFS= read -r archive; do
		sigs=$(find "$(dirname "$archive")" -name "$(basename "$archive").*" \( -name '*.sig' -o -name '*.rmd160' \) | wc -l | tr -d ' ')
		[ "$sigs" -ge 1 ] || bad="$bad $(basename "$archive")(unsigned)"
	done <<EOT
$(find "$archives" -type f ! -name '*.sig' ! -name '*.rmd160')
EOT
	if [ -n "$bad" ]; then
		row_fail "archives without their signatures:$bad"
	elif [ -n "$(join -1 2 -2 2 "$ROW_DIR/c9.first" "$ROW_DIR/c9.second" | awk '$2 != $3')" ]; then
		row_fail "the second check signed again what the first had signed"
	else
		row_pass "every kept archive is signed beside it, once"
	fi
}
