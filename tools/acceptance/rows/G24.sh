# stages: quick full
# G24: what outdated says of one port beside "no newer release" or its
# update (v0.8.0), each over the network, at the stage's pinned master:
#  - a repository GitHub's web answers under another name: gitui's
#    github.setup names extrawurst/gitui, which GitHub answers as
#    gitui-org/gitui, said on the one-port line and as repository_move in
#    --json (68b05cd7, f8aec41f);
#  - a look whose only match is a prerelease: php-igbinary, whose php
#    PortGroup's livecheck hook is now read as known (06d0e068), lists
#    3.2.17RC1 above its 3.2.16, said as a prerelease a port on a stable
#    release doesn't take, and as prerelease in --json (83956939);
#  - a version captured from a prerelease's name is a prerelease:
#    ap-utils' livecheck captures 1.4.2 from 1.4.2pre1, and the update
#    offered is the stable 1.4.1 (feaf65b2, the outdated-ports session's
#    note 19).
# Where upstream has since moved on (a stable php-igbinary past 3.2.16, a
# stable ap-utils past 1.4.1), the row isn't run, saying which.
act() {
	dh outdated gitui </dev/null || :
	dh_json outdated gitui </dev/null || :
	dh outdated php-igbinary </dev/null || :
	dh_json outdated php-igbinary </dev/null || :
	dh_json outdated ap-utils </dev/null || :
}
assert() {
	local move=$ROW_DIR/json/1.json pre=$ROW_DIR/json/2.json captured=$ROW_DIR/json/3.json
	[ -f "$captured" ] || { row_fail "outdated didn't run each port"; return; }
	if ! grep -A2 -F '$ dockhand outdated gitui' "$ROW_DIR/out.log" | tr '\n' ' ' | sed 's/  */ /g' | matches 'GitHub answers extrawurst/gitui as gitui-org/gitui'; then
		row_fail "outdated gitui's one-port line didn't say GitHub's answer: $(grep -A2 -F '$ dockhand outdated gitui' "$ROW_DIR/out.log" | sed -n 2,3p | tr '\n' ' ')"
		return
	fi
	if [ "$(jq -r '.result.ports[0].repository_move.answered_as // empty' "$move")" != gitui-org/gitui ]; then
		row_fail "outdated --json gitui carried no repository_move answered as gitui-org/gitui: $(jq -c '.result.ports[0]' "$move")"
		return
	fi
	if [ "$(jq -r '.result.ports[0].outdated' "$pre")" = true ]; then
		row_result "not run" "php-igbinary has a newer release upstream now, $(jq -r '.result.ports[0].newest' "$pre"), so its look isn't of a prerelease alone"
		return
	fi
	if ! grep -A2 -F '$ dockhand outdated php-igbinary' "$ROW_DIR/out.log" | tr '\n' ' ' | sed 's/  */ /g' | matches "is a prerelease, which a port on a stable release doesn't take"; then
		row_fail "outdated php-igbinary didn't say its only match is a prerelease: $(grep -A2 -F '$ dockhand outdated php-igbinary' "$ROW_DIR/out.log" | sed -n 2,3p | tr '\n' ' ')"
		return
	fi
	if [ -z "$(jq -r '.result.ports[0].prerelease // empty' "$pre")" ]; then
		row_fail "outdated --json php-igbinary carried no prerelease: $(jq -c '.result.ports[0]' "$pre")"
		return
	fi
	case "$(jq -r '.result.ports[0].newest // empty' "$captured")" in
	1.4.1) ;;
	1.4.2)
		row_fail "outdated ap-utils offered 1.4.2, read from 1.4.2pre1, a prerelease's name"
		return
		;;
	*)
		row_result "not run" "ap-utils' newest upstream is $(jq -r '.result.ports[0].newest // "unknown"' "$captured") now, not 1.4.1 beside 1.4.2pre1"
		return
		;;
	esac
	row_pass "gitui: $(jq -r '.result.ports[0].repository_move.words' "$move"); php-igbinary current at $(jq -r '.result.ports[0].current' "$pre"), upstream's newest $(jq -r '.result.ports[0].prerelease' "$pre") a prerelease; ap-utils offered 1.4.1, not 1.4.2pre1's 1.4.2"
}
