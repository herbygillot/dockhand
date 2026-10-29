# stages: quick full
# G10: plain-HTTP links switched to HTTPS where they answer (v0.7.0). As
# at the quick stage's pin, dnsmasq's homepage and master_sites are plain
# HTTP, and each answers over https, the site for the distfile it serves:
# its update switches both, in the Portfile and in what it says. mutt's
# don't answer there: its update switches neither and says them as before.
# dnsmasq's update again with --keep-http, through a proxy that logs each
# host it's asked for, asks none of its hosts over https and says nothing
# of them. Each branch first sets its port's directory back to the pin's,
# so a day's master, which has these versions, still has them to update.
. "${ROW_LIB:?}/fault.sh"
g10_pin() { printf '%s' "${ACCEPT_PIN:-026b3878a258b85619633709d89871f2f8750b94}"; }
https_port() { printf 'dnsmasq'; }
https_dir() { printf 'net/dnsmasq'; }
https_version() { printf '2.93'; }
https_host() { printf 'www.thekelleys.org.uk'; }
plain_port() { printf 'mutt'; }
plain_dir() { printf 'mail/mutt'; }
plain_version() { printf '2.4.2'; }

# g10_branch starts a branch of a port, its directory as at the pin, and
# says its name.
g10_branch() {
	local name=$1 port=$2 dir=$3 worktree
	dh_setup start "$name" --port "$port" || return 1
	worktree=$("$DH_BIN" path "$name") || { row_fail "dockhand path $name found no worktree"; return 1; }
	git -C "$worktree" checkout "$(g10_pin)" -- "$dir" || { row_fail "$dir isn't at the pin, $(g10_pin), in $worktree"; return 1; }
	git -C "$worktree" diff --cached --quiet && return 0
	git -C "$worktree" -c user.name=acceptance -c user.email=acceptance@example.invalid -c commit.gpgsign=false \
		commit -q -m "$port: as at the acceptance pin" || { row_fail "couldn't commit $dir as at the pin"; return 1; }
}

act() {
	G10_SWITCHED=$(run_name g10-https)
	G10_PLAIN=$(run_name g10-plain)
	G10_KEPT=$(run_name g10-keep-http)
	g10_branch "$G10_SWITCHED" "$(https_port)" "$(https_dir)" || return 0
	dh_json update "$(https_port)" "$(https_version)" -b "$G10_SWITCHED" || return 0
	g10_branch "$G10_PLAIN" "$(plain_port)" "$(plain_dir)" || return 0
	dh_json update "$(plain_port)" "$(plain_version)" -b "$G10_PLAIN" || return 0
	g10_branch "$G10_KEPT" "$(https_port)" "$(https_dir)" || return 0
	local proxy
	proxy=$(fault_proxy pass) || return 1
	(
		export HTTPS_PROXY="$proxy" https_proxy="$proxy"
		dh_json update "$(https_port)" "$(https_version)" -b "$G10_KEPT" --keep-http || :
	)
	fault_proxy_stop
}

# g10_update is an update's envelope's own change, alone or a batch's.
g10_update() { jq -c '.result.changes[0]? // .result' "$ROW_DIR/json/$1.json" 2>/dev/null; }

# g10_portfile is a branch's Portfile of a port, as its worktree has it.
g10_portfile() {
	local dir
	dir=$("$DH_BIN" path "$1" 2>/dev/null) || return 1
	cat "$dir/$2/Portfile" 2>/dev/null
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	local n file
	for n in 1 2 3; do
		file=$ROW_DIR/json/$n.json
		[ "$(cat "$file.exit" 2>/dev/null)" = 0 ] || { row_fail "dockhand $(cat "$file.args" 2>/dev/null) exited $(cat "$file.exit" 2>/dev/null): $(jq -r '.error // empty' "$file" 2>/dev/null)"; return; }
	done
	local switched plain kept portfile option
	switched=$(g10_update 1)
	for option in homepage master_sites; do
		printf '%s' "$switched" | jq -e --arg option "$option" '[.switched_to_https[]?.option] | index($option)' >/dev/null ||
			{ row_fail "$(https_port)'s update didn't say its $option switched to HTTPS: $(printf '%s' "$switched" | jq -c '{switched_to_https, plain_http}')"; return; }
	done
	portfile=$(g10_portfile "$G10_SWITCHED" "$(https_dir)") || { row_fail "no Portfile of $(https_port) on $G10_SWITCHED"; return; }
	printf '%s\n' "$portfile" | matches -E '^homepage[[:space:]]+https://' && printf '%s\n' "$portfile" | matches -E '^master_sites[[:space:]]+https://' ||
		{ row_fail "$(https_port)'s Portfile on $G10_SWITCHED isn't switched: $(printf '%s\n' "$portfile" | grep -E '^(homepage|master_sites)' | tr '\n' ';')"; return; }
	plain=$(g10_update 2)
	printf '%s' "$plain" | jq -e '(.switched_to_https | length) == 0 and ([.plain_http[]? | select(.https_answers | not) | .option] | index("master_sites"))' >/dev/null ||
		{ row_fail "$(plain_port)'s update didn't say its master_sites doesn't answer over https, switching nothing: $(printf '%s' "$plain" | jq -c '{switched_to_https, plain_http}')"; return; }
	portfile=$(g10_portfile "$G10_PLAIN" "$(plain_dir)") || { row_fail "no Portfile of $(plain_port) on $G10_PLAIN"; return; }
	printf '%s\n' "$portfile" | matches -E '^master_sites[[:space:]]+http://' || { row_fail "$(plain_port)'s master_sites was changed though it doesn't answer over https"; return; }
	kept=$(g10_update 3)
	printf '%s' "$kept" | jq -e '(.switched_to_https | length) == 0 and (.plain_http | length) == 0' >/dev/null ||
		{ row_fail "--keep-http said its links: $(printf '%s' "$kept" | jq -c '{switched_to_https, plain_http}')"; return; }
	portfile=$(g10_portfile "$G10_KEPT" "$(https_dir)") || { row_fail "no Portfile of $(https_port) on $G10_KEPT"; return; }
	printf '%s\n' "$portfile" | matches -E '^homepage[[:space:]]+http://' || { row_fail "--keep-http switched $(https_port)'s homepage"; return; }
	# The download's own redirect, http://www.… to https://thekelleys…,
	# goes through the proxy too, which shows it's in use; an https ask of
	# the links themselves would be of their own host.
	matches CONNECT "$ROW_DIR/faultproxy.log" 2>/dev/null || { row_result "not run" "the proxy logged no request: dockhand's https didn't go through it, so it can't show --keep-http asked nothing"; return; }
	if matches -F "CONNECT $(https_host):443" "$ROW_DIR/faultproxy.log"; then
		row_fail "--keep-http asked $(https_host) over https: $(grep CONNECT "$ROW_DIR/faultproxy.log" | tr '\n' ';')"
		return
	fi
	row_pass "$(https_port)'s homepage and master_sites were switched to https and said; $(plain_port)'s, which don't answer there, were left and said as before; --keep-http asked $(https_host) nothing and said nothing"
}
