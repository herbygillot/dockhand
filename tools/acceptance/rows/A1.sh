# stages: full
# A1: install from a local copy of the dockhand Portfile at the candidate
# commit, which host/stage-candidate.sh put in the overlay. It builds, and
# dockhand --version names the candidate's version, which tests the
# ldflag. A release tarball has no Git data, so a tagged build names no
# commit, and needn't: its tag names one (internal/buildinfo, Source),
# which a Generated-By trailer is found by.
act() {
	checkpoint "install the candidate: sudo port -N install dockhand, from the overlay" || return 0
	dh --version || :
}
assert() {
	local version
	version=$(sed -n '/^\$ dockhand --version/,/^\[exit/p' "$ROW_DIR/out.log" | sed -n 2p)
	case "$version" in
	*"${ACCEPT_CANDIDATE#v}"*) row_pass "dockhand --version names the candidate: $version" ;;
	*) row_fail "dockhand --version says \"$version\", not ${ACCEPT_CANDIDATE}" ;;
	esac
}
