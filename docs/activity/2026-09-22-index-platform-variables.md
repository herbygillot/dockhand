# 2026-09-22: the indexer is told the platform the way the buildbot tells it

## The defect

`portindex -p plat_ver_arch` sets `os_arch` verbatim from the third field
(`portindex`, lines 336 to 354 of the installed 2.12.6 script), and
dockhand passed `darwin_25_arm64`. MacPorts base sets `os_arch` to what
`uname -p` reports, `arm` on Apple silicon and `i386` on every Intel
machine (`macports1.0/macports.tcl`, lines 1090 to 1103), and names the
mirror's index directory by it, `PortIndex_darwin_25_arm`. So every local
generation evaluated `${os.arch}` with a value base never produces. In
the tree at 2026-09-22, 116 Portfiles and 11 PortGroups test `os.arch`,
twenty of them `eq "arm"` and six `ne "arm"`, and the compilers and
common_lisp PortGroups gate on it; the dependencies those conditions
choose are what the index records and dependent discovery reads.

Two smaller mismatches sat beside it. The `darwin_` form skips the
`os_subplatform` and `cxx_stdlib` overrides the `macosx_` form sets, and
a bare triple leaves `build_arch` at the host's value, so an index
generated here for an x86_64 image would have said `os_arch x86_64
build_arch arm64`.

The official infrastructure does neither. The ports mirror workflow
passes `macosx_${major}_$(uname -p)`, and `file:mpbb/index_vars/<that>`
when mpbb ships a variables file for the platform, which says `os_arch
arm build_arch arm64` along with the release's `universal_archs`, macOS
and SDK versions, and compiler caches, so the index resembles a real run
there. The defect was first recorded in the [bootstrap
note](2026-09-21-index-cache-bootstrap.md) and listed as open on
2026-09-22; this note verifies it against those sources.

## The fix

One table, `macports.PlatformVariables`, produces the list of variable
and value pairs that make an interpreter describe another platform:
`os_platform`, `os_subplatform`, `os_major`, `os_version`, `os_arch` as
`uname -p` would report it, `build_arch`, the macOS product version in
its three spellings, the deployment target, `universal_archs`, and
`cxx_stdlib`, following base for each. The indexer receives it as
`-p file:` the way the buildbot does, and the modeled observation
receives the same list from Go instead of computing its own in Tcl, so
the two paths cannot disagree. Which platforms are supported is decided
in Go; the Tcl setup refuses only a malformed list.

The variables are part of the index cache's identity, so generations
built under the old description are not reused, and the environment
file beside a cache's generations records them.

## Checked

`games/lbreakouthd` appends autoconf, automake, and libtool to its build
dependencies under `${os.arch} eq "arm"`. Every generation in the old
cache identity, six of them, records its build dependencies without the
three. The installed indexer run on the port both ways agrees: the bare
`darwin_25_arm64` leaves them out, and the variables file puts them in.
The first command after the fix opened a new cache identity, seeded it
from the mirror, and recorded the variables in its environment file.
