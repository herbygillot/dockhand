#!/usr/bin/env bash
# make-gone-fixture.sh makes the fixture D-N2 reads (the person's "Sure,
# we can do this.", 2026-11-04): a small public project of the test
# account's with releases v1.0.0 and v1.1.0, each carrying one asset,
# <name>-<version>.tar.gz, v1.0.0's asset then deleted, its tag and
# release kept, so the old distfile 404s while dockhand's lookups list
# both releases. It writes the port's Portfile, at 1.0.0, fetching the
# release asset with v1.0.0's checksums, into the host's fixtures
# directory, which D-N2 copies into its branch's tree, as G36 does
# make-source-fixture.sh's; full.sh names the project as ACCEPT_DN2_FIXTURE.
#
# Once made it is static: D-N2 changes nothing on GitHub, so nothing is
# re-tagged or deleted per run. It makes only what isn't there: a
# repository, a tag, a release, or an asset that exists is left as it is
# and said, and v1.0.0's asset is deleted once the Portfile holds its
# checksums. It runs as the account that owns the fixture, gh logged in
# as it, and pushes with gh's own credential, as make-source-fixture.sh
# does. Without --run, it only says what it would do.
#
#   tools/acceptance/host/make-gone-fixture.sh [--run]
#
#   ACCEPT_FIXTURE_OWNER  the account, herbyg-test unless named
#   ACCEPT_DN2_NAME       the project, dockhand-gone-fixture unless named
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"

owner=${ACCEPT_FIXTURE_OWNER:-herbyg-test}
name=${ACCEPT_DN2_NAME:-dockhand-gone-fixture}
repo=$owner/$name
fixtures="$HOST_ROOT/fixtures"
portfile="$fixtures/$name/Portfile"
old=$name-1.0.0.tar.gz
new=$name-1.1.0.tar.gz

command -v gh >/dev/null || die "gh isn't on PATH; install it and log it in as $owner"
login=$(gh api user --jq .login 2>/dev/null) || die "gh has no login: log it in as $owner (gh auth login), or set GH_TOKEN to $owner's token"
[ "$login" = "$owner" ] || die "gh is logged in as $login, not $owner, whose fixture this is"

# The fixtures directory, or the nearest directory above it that exists,
# must be this account's to write, as make-source-fixture.sh says.
writable=$fixtures
while [ ! -e "$writable" ]; do writable=$(dirname "$writable"); done
[ -w "$writable" ] || die "$writable belongs to $(stat -f %Su "$writable"), and $(id -un) can't write $portfile there: make $fixtures this account's to write (sudo mkdir -p $fixtures && sudo chown $(id -un) $fixtures), or name another ACCEPT_HOST_ROOT"

exists() { gh api "repos/$repo" --silent 2>/dev/null; }
empty() { [ "$(gh api "repos/$repo/branches" --jq length 2>/dev/null)" = 0 ]; }
# assets names a release's assets, one a line; a release that isn't there
# fails.
assets() { gh release view "$1" --repo "$repo" --json assets --jq '.assets[].name' 2>/dev/null; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git_in() { git -C "$1" -c user.name="$owner" -c user.email="$owner@users.noreply.github.com" -c commit.gpgsign=false -c tag.gpgsign=false "${@:2}"; }
push() {
	local dir=$1
	shift
	git_in "$dir" -c credential.helper= -c 'credential.helper=!gh auth git-credential' push -q "https://github.com/$repo.git" "$@"
}

# archive writes a tag's archive, one top directory <name>-<version>, as
# the release asset is: gzip -n, so a rebuild from the tag is the same
# bytes on the same git.
archive() {
	local version=$1
	[ -d "$work/clone" ] || git clone -q "https://github.com/$repo.git" "$work/clone"
	git_in "$work/clone" archive --format=tar --prefix="$name-$version/" "v$version" | gzip -n >"$work/$name-$version.tar.gz"
}

# 1. The project: a Makefile whose install copies VERSION, and tags v1.0.0
# and v1.1.0.
if exists && ! empty; then
	echo "$repo is there; left as it is"
elif [ "$DRY" = 1 ]; then
	echo "would create $repo, public, with a Makefile and VERSION, and tags v1.0.0 and v1.1.0, and push them"
else
	mkdir "$work/main"
	git_in "$work/main" init -q -b main
	cat >"$work/main/Makefile" <<'MAKE'
PREFIX ?= /usr/local

all:

install:
	mkdir -p $(DESTDIR)$(PREFIX)/share/gone-fixture
	cp VERSION $(DESTDIR)$(PREFIX)/share/gone-fixture/VERSION
MAKE
	printf '%s\n' "dockhand's acceptance fixture: a project whose v1.0.0 release asset is gone, for an update whose old distfile 404s." >"$work/main/README"
	printf '1.0.0\n' >"$work/main/VERSION"
	git_in "$work/main" add Makefile README VERSION
	git_in "$work/main" commit -q -m "1.0.0"
	git_in "$work/main" tag -a v1.0.0 -m v1.0.0
	printf '1.1.0\n' >"$work/main/VERSION"
	git_in "$work/main" commit -q -am "1.1.0"
	git_in "$work/main" tag -a v1.1.0 -m v1.1.0
	exists || step gh repo create "$repo" --public --description "dockhand's acceptance fixture: a release whose old distfile is gone"
	step push "$work/main" main v1.0.0 v1.1.0
fi

# 2. The releases, each with its one asset. v1.0.0's is made only where
# its release isn't there: a release without it is the fixture made.
for version in 1.0.0 1.1.0; do
	asset=$name-$version.tar.gz
	if listed=$(assets "v$version"); then
		if printf '%s\n' "$listed" | grep -qxF "$asset"; then
			echo "$repo's release v$version is there, with $asset; left as it is"
		else
			echo "$repo's release v$version is there without $asset; left as it is"
		fi
	elif [ "$DRY" = 1 ]; then
		echo "would make $repo's release v$version, with $asset"
	else
		archive "$version"
		step gh release create "v$version" --repo "$repo" --title "v$version" --notes "dockhand's acceptance fixture." "$work/$asset"
	fi
done

# 3. The port, at 1.0.0, fetching the release asset, with v1.0.0's
# checksums: the asset's own where it's still there, else the tag's
# archive made again, which is the same bytes on the same git.
if [ -f "$portfile" ]; then
	echo "$portfile is there; left as it is"
elif [ "$DRY" = 1 ]; then
	echo "would write $portfile at 1.0.0, github.tarball_from releases, with $old's checksums"
else
	if assets v1.0.0 | grep -qxF "$old"; then
		gh release download v1.0.0 --repo "$repo" --pattern "$old" --dir "$work/download"
		file=$work/download/$old
	else
		echo "$repo's release v1.0.0 has no $old; its checksums are of the tag's archive, made again"
		archive 1.0.0
		file=$work/$old
	fi
	rmd160=$(/usr/bin/openssl dgst -rmd160 -r "$file" | cut -d' ' -f1)
	sha256=$(shasum -a 256 "$file" | cut -d' ' -f1)
	size=$(stat -f %z "$file")
	mkdir -p "$(dirname "$portfile")"
	cat >"$portfile" <<PORTFILE
# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4

PortSystem          1.0
PortGroup           github 1.0

github.setup        $owner $name 1.0.0 v
github.tarball_from releases
revision            0

description         dockhand's acceptance fixture
long_description    A project whose first release asset is gone, for an \\
                    update whose old distfile 404s.

categories          devel
license             MIT
maintainers         nomaintainer
platforms           any
supported_archs     noarch

checksums           rmd160  $rmd160 \\
                    sha256  $sha256 \\
                    size    $size

use_configure       no
destroot.args       PREFIX=\${prefix}
PORTFILE
	echo "wrote $portfile"
fi

# 4. v1.0.0's asset deleted, its tag and release kept, once the Portfile
# holds its checksums.
if listed=$(assets v1.0.0); then
	if printf '%s\n' "$listed" | grep -qxF "$old"; then
		step gh release delete-asset v1.0.0 "$old" --repo "$repo" --yes
	else
		echo "$repo's release v1.0.0 has no $old, so its distfile 404s"
	fi
else
	echo "would delete $old from $repo's release v1.0.0 once it's made, keeping the tag and release"
fi
then_person "full.sh reads ACCEPT_DN2_FIXTURE=$repo, and D-N2 copies $portfile into its branch as devel/$name/Portfile"
