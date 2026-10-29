# stages: full
# G16: check's plan estimates what it installs (v0.7.0). check --plan --on
# tart of the small Go port says "Installs    about N dependencies to
# install, M with no archive on packages.macports.org for this release, by
# the port index; the clone decides"; the check then runs, and a person
# compares the estimate with what the guest installed, as its log says.
port() { printf '%s' "${ACCEPT_GO_PORT:?}"; }

act() {
	host_only "a Tart check" || return 0
	G16_BRANCH=$(run_name g16-estimate)
	dh_setup revbump "$(port)" --new="$G16_BRANCH" --subject "rebuild to compare the install estimate" || return 0
	dh check -b "$G16_BRANCH" --on tart --plan || :
	dh check -b "$G16_BRANCH" --on tart -v || :
	"$DH_BIN" logs -b "$G16_BRANCH" --port "$(port)" >"$ROW_DIR/port.log" 2>&1 || :
}

assert() {
	[ -f "$ROW_DIR/result" ] && return 0
	[ -n "${G16_BRANCH:-}" ] || return 0
	local estimate installed
	estimate=$(sed -n '/^\$ dockhand check .*--plan/,/^\[exit/p' "$ROW_DIR/out.log" | grep -E '^Installs +about [0-9]+ dependenc(y|ies) to install, [0-9]+ with no archive on packages\.macports\.org for this release, by the port index; the clone decides$' | head -1)
	[ -n "$estimate" ] || { row_fail "check --plan said no estimate with its archives: $(grep -E '^Installs' "$ROW_DIR/out.log" | head -2 | tr '\n' ' ')"; return; }
	installed=$(grep -cE 'Installing [a-z]' "$ROW_DIR/port.log" 2>/dev/null || echo 0)
	judged "the plan's estimate, \"${estimate#Installs    }\", is close to what the guest installed, as $(port)'s log in $ROW_DIR/port.log says ($installed Installing lines)"
}
