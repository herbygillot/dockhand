# stages: full
# G19: a selection's ports compared with port echo's on the M1's tree
# (v0.7.0's selectors). outdated --current --maintainer and --category,
# and --not-maintainer, select the ports port echo's maintainer: and
# category: select, combined as MacPorts combines them: the same names, but
# for ports the tree and the prefix's index disagree on, which a person
# reads. ACCEPT_MAINTAINER names the maintainer, @herbygillot unless set,
# and ACCEPT_CATEGORY the category, textproc unless set.
maintainer() { printf '%s' "${ACCEPT_MAINTAINER:-@herbygillot}"; }
category() { printf '%s' "${ACCEPT_CATEGORY:-textproc}"; }

act() {
	host_only "port echo against the M1's MacPorts" || return 0
	command -v port >/dev/null || { row_result "not run" "no port command on PATH"; return 0; }
	local handle
	handle=$(maintainer)
	handle=${handle#@}
	dh_json outdated --current --maintainer "$(maintainer)" || :
	port -q echo "maintainer:$handle" 2>/dev/null | awk '{print $1}' | sort -u >"$ROW_DIR/echo.maintainer"
	dh_json outdated --current --category "$(category)" --not-maintainer "$(maintainer)" || :
	port -q echo "category:$(category)" and not "maintainer:$handle" 2>/dev/null | awk '{print $1}' | sort -u >"$ROW_DIR/echo.category"
}

# g19_compare says where dockhand's selection and port echo's differ.
g19_compare() {
	local json=$1 echo=$2 name=$3
	jq -r '.result.ports[]?.port' "$json" 2>/dev/null | sort -u >"$ROW_DIR/dockhand.$name"
	comm -3 "$ROW_DIR/dockhand.$name" "$echo" >"$ROW_DIR/differ.$name"
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -s "$ROW_DIR/echo.maintainer" ] || { row_result "not run" "port echo selected nothing for $(maintainer)"; return; }
	g19_compare "$ROW_DIR/json/1.json" "$ROW_DIR/echo.maintainer" maintainer
	g19_compare "$ROW_DIR/json/2.json" "$ROW_DIR/echo.category" category
	local maintainer category
	maintainer=$(wc -l <"$ROW_DIR/differ.maintainer" | tr -d ' ')
	category=$(wc -l <"$ROW_DIR/differ.category" | tr -d ' ')
	if [ "$maintainer" = 0 ] && [ "$category" = 0 ]; then
		row_pass "outdated's selections matched port echo's: $(wc -l <"$ROW_DIR/echo.maintainer" | tr -d ' ') ports of $(maintainer), and $(wc -l <"$ROW_DIR/echo.category" | tr -d ' ') of $(category) not theirs"
		return
	fi
	judged "outdated's selections differ from port echo's only where the stage's tree and /opt/local's index disagree: $maintainer for $(maintainer), $category for $(category), listed in $ROW_DIR/differ.*"
}
