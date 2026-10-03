# stages: quick full
# C9: kept archives and their signatures. A check keeps what it built. An
# archive is signed beside it when it's first served to a guest as a
# dependency (binaryarchive.Sign), once, not per check: after two checks
# of the small Go port, every signature there is one per key, and the
# second check signed nothing again. The small Go port depends on no kept
# archive, so its own may stay unsigned; a guest installing one as a
# dependency is the full stage's to see.
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
		sigs=$(find "$(dirname "$archive")" -name "$(basename "$archive").*.sig" | wc -l | tr -d ' ')
		[ "$sigs" -le 1 ] || bad="$bad $(basename "$archive")($sigs signify signatures)"
		sigs=$(find "$(dirname "$archive")" -name "$(basename "$archive").*.rmd160" | wc -l | tr -d ' ')
		[ "$sigs" -le 1 ] || bad="$bad $(basename "$archive")($sigs RSA signatures)"
	done <<EOT
$(find "$archives" -type f ! -name '*.sig' ! -name '*.rmd160')
EOT
	if [ -n "$bad" ]; then
		row_fail "archives signed more than once a key:$bad"
	elif [ -n "$(join -1 2 -2 2 "$ROW_DIR/c9.first" "$ROW_DIR/c9.second" | awk '$2 != $3')" ]; then
		row_fail "the second check signed again what the first had signed"
	else
		row_pass "archives kept, $(find "$archives" -name '*.sig' | wc -l | tr -d ' ') signed, none twice, and nothing signed again"
	fi
}
