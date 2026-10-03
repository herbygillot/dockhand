#!/usr/bin/env bash
# stage-candidate.sh writes the dockhand Portfile for a release candidate
# into the host's overlay of ports, at the rc tag, with its checksums and
# the version ldflag, internal/buildinfo.Version: the Portfile
# macports/macports-ports#34756 will carry, so A1 tests the port itself.
# It runs nothing as dhtest; A1 does the install. Without --run, it only
# says what it would write.
#
#   tools/acceptance/host/stage-candidate.sh v0.3.0-rc1 [--run]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"

rc=${1:-}
case "$rc" in v[0-9]*.[0-9]*.[0-9]*-rc[0-9]*) ;; *) die "name the candidate's tag, such as v0.3.0-rc1, not ${rc:-nothing}" ;; esac
version=${rc#v}
url="https://github.com/herbygillot/dockhand/archive/refs/tags/$rc.tar.gz"
portfile="$HOST_ROOT/ports/sysutils/dockhand/Portfile"

if [ "$DRY" = 1 ]; then
	echo "would fetch $url and compute its rmd160, sha256, and size"
	rmd160=RMD160 sha256=SHA256 size=SIZE
else
	tarball=$(mktemp)
	trap 'rm -f "$tarball"' EXIT
	curl -fsSL -o "$tarball" "$url"
	rmd160=$(openssl dgst -rmd160 -r "$tarball" | cut -d' ' -f1)
	sha256=$(shasum -a 256 "$tarball" | cut -d' ' -f1)
	size=$(stat -f %z "$tarball")
fi
text=$(sed -e "s/@VERSION@/$version/" -e "s/@RMD160@/$rmd160/" -e "s/@SHA256@/$sha256/" -e "s/@SIZE@/$size/" "$here/Portfile.in")
if [ "$DRY" = 1 ]; then
	echo "would write $portfile:"
	printf '%s\n' "$text" | sed 's/^/    /'
else
	mkdir -p "$(dirname "$portfile")"
	printf '%s\n' "$text" >"$portfile"
	(cd "$HOST_ROOT/ports" && /opt/local/bin/portindex >/dev/null)
	echo "staged $rc in $portfile"
fi
