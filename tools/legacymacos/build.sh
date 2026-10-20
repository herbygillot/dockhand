#!/bin/sh
# Build dockhand for macOS 10.13 to 11: this Go, with x509.patch applied to
# its crypto/x509 through go build -overlay, which leaves the toolchain
# itself untouched. Then check each binary's imports (legacymacos).
#
# usage: build.sh OUTDIR [ARCH...]
#
# It builds the dockhand source in the current directory, which needn't be
# this script's: release.yml builds an older tag with main's patch. GO names
# the go command, and GO_LDFLAGS the -ldflags, as the Makefile's; the build
# is linked as buildinfo.Variant "legacy" too, which a tag from before the
# variable ignores.
set -eu

if [ $# -lt 1 ]; then
	echo "usage: build.sh OUTDIR [ARCH...]" >&2
	exit 2
fi
here=$(cd "$(dirname "$0")" && pwd)
out=$1
shift
archs=${*:-amd64 arm64}
GO=${GO:-go}
hostgo=$GO

if [ ! -d cmd/dockhand ]; then
	echo "build.sh builds the dockhand source in the current directory, and $(pwd) has none" >&2
	exit 2
fi
mkdir -p "$out"
out=$(cd "$out" && pwd)
version=$("$GO" env GOVERSION)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# go build refuses an overlay of files beneath the module cache, where a
# toolchain GOTOOLCHAIN downloaded lives, so such a one is built from a copy.
goroot=$("$GO" env GOROOT)
case $goroot in
"$("$GO" env GOMODCACHE)"/*)
	cp -R "$goroot" "$work/goroot"
	chmod -R u+w "$work/goroot"
	goroot=$work/goroot
	;;
esac
GO=$goroot/bin/go
export GOTOOLCHAIN=local

files="src/crypto/x509/root_darwin.go src/crypto/x509/internal/macos/security.go src/crypto/x509/internal/macos/security.s"
replace=
for file in $files; do
	mkdir -p "$work/patched/$(dirname "$file")"
	cp "$goroot/$file" "$work/patched/$file"
	replace="$replace${replace:+,}\"$goroot/$file\":\"$work/patched/$file\""
done
# No fuzz: a Go release that changes these lines needs the patch looked at
# again, not applied near them.
if ! patch -s -F0 -p1 -d "$work/patched" <"$here/x509.patch"; then
	echo "x509.patch doesn't apply to $version's crypto/x509; see tools/legacymacos/README.md" >&2
	exit 1
fi
printf '{"Replace":{%s}}\n' "$replace" >"$work/overlay.json"

ldflags="${GO_LDFLAGS:-}${GO_LDFLAGS:+ }-X github.com/herbygillot/dockhand/internal/buildinfo.Variant=legacy"
binaries=
for arch in $archs; do
	binary=$out/dockhand-legacy-darwin-$arch
	GOOS=darwin GOARCH=$arch CGO_ENABLED=0 "$GO" build -overlay "$work/overlay.json" \
		-ldflags "$ldflags" -o "$binary" ./cmd/dockhand
	binaries="$binaries $binary"
done
# The check is this script's, run in its own checkout, with the Go its
# go.mod asks for.
# shellcheck disable=SC2086 # the binaries' paths, split as listed
(cd "$here/../.." && GOTOOLCHAIN=auto "$hostgo" run ./tools/legacymacos $binaries)
echo "built$binaries"
