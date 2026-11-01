#!/usr/bin/env bash
# make-source-fixture.sh makes the fixture G35's full-stage case and the
# Tart check of a moved port read (the person's "You can create the test
# repository", 2026-11-01): a small public project of the test account's,
# fetched by its port with Git for its one submodule, whose v1.1.0 release
# carries two archives, <name>_1.1.0_tarball.tgz holding the submodule's
# files, which an update's source choice takes, and <name>-1.1.0.tar.gz
# without them, which it says lacks the submodule. It writes the port's
# Portfile, at 1.0.0, into the host's fixtures directory, which a row
# copies into its branch's tree, since the stage's ports clone is made
# anew each run; full.sh names the project as ACCEPT_SOURCE_FIXTURE.
#
# It makes only what isn't there: a repository, a tag, a release, or an
# asset that exists is left as it is, never changed, and said. It runs as
# the account that owns the fixture, gh logged in as it. Without --run, it
# only says what it would do.
#
#   tools/acceptance/host/make-source-fixture.sh [--run]
#
#   ACCEPT_FIXTURE_OWNER  the account, herbyg-test unless named
#   ACCEPT_FIXTURE_NAME   the project, dockhand-source-fixture unless named
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"

owner=${ACCEPT_FIXTURE_OWNER:-herbyg-test}
name=${ACCEPT_FIXTURE_NAME:-dockhand-source-fixture}
sub=$name-sub
repo=$owner/$name
fixtures="$HOST_ROOT/fixtures"
portfile="$fixtures/$name/Portfile"

command -v gh >/dev/null || die "gh isn't on PATH; install it and log it in as $owner"
login=$(gh api user --jq .login 2>/dev/null) || die "gh has no login: log it in as $owner (gh auth login), or set GH_TOKEN to $owner's token"
[ "$login" = "$owner" ] || die "gh is logged in as $login, not $owner, whose fixture this is"

# exists says whether a repository of the account's is on GitHub.
exists() { gh api "repos/$owner/$1" --silent 2>/dev/null; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# git_in runs git in a directory with an identity of the fixture's, so the
# commits don't carry this Mac's user.
git_in() { git -C "$1" -c user.name="$owner" -c user.email="$owner@users.noreply.github.com" -c commit.gpgsign=false -c tag.gpgsign=false "${@:2}"; }

# 1. The submodule's project: one file the main project's build installs.
if exists "$sub"; then
	echo "$owner/$sub is there; left as it is"
elif [ "$DRY" = 1 ]; then
	echo "would create $owner/$sub, public, holding share.txt, and push its main"
else
	mkdir "$work/sub"
	git_in "$work/sub" init -q -b main
	printf 'carried by the submodule\n' >"$work/sub/share.txt"
	git_in "$work/sub" add share.txt
	git_in "$work/sub" commit -q -m "the submodule's file"
	step gh repo create "$owner/$sub" --public --description "dockhand's acceptance fixture: a submodule"
	step git_in "$work/sub" push -q "https://github.com/$owner/$sub.git" main
fi

# 2. The project: a Makefile whose install needs the submodule's file, the
# submodule at deps/sub, and tags v1.0.0 and v1.1.0.
if exists "$name"; then
	echo "$repo is there; left as it is"
elif [ "$DRY" = 1 ]; then
	echo "would create $repo, public, with a Makefile, deps/sub as a submodule of $owner/$sub, and tags v1.0.0 and v1.1.0, and push them"
else
	mkdir "$work/main"
	git_in "$work/main" init -q -b main
	cat >"$work/main/Makefile" <<'MAKE'
PREFIX ?= /usr/local

all:
	test -f deps/sub/share.txt

install: all
	mkdir -p $(DESTDIR)$(PREFIX)/share/fixture
	cp deps/sub/share.txt $(DESTDIR)$(PREFIX)/share/fixture/share.txt
	cp VERSION $(DESTDIR)$(PREFIX)/share/fixture/VERSION
MAKE
	printf '%s\n' "dockhand's acceptance fixture: a project fetched with Git for its submodule, whose release carries an archive holding it." >"$work/main/README"
	printf '1.0.0\n' >"$work/main/VERSION"
	git_in "$work/main" -c protocol.file.allow=always submodule add -q "https://github.com/$owner/$sub.git" deps/sub
	git_in "$work/main" add Makefile README VERSION .gitmodules deps/sub
	git_in "$work/main" commit -q -m "1.0.0"
	git_in "$work/main" tag -a v1.0.0 -m v1.0.0
	printf '1.1.0\n' >"$work/main/VERSION"
	git_in "$work/main" commit -q -am "1.1.0"
	git_in "$work/main" tag -a v1.1.0 -m v1.1.0
	step gh repo create "$repo" --public --description "dockhand's acceptance fixture: a project fetched with Git for its submodule"
	step git_in "$work/main" push -q "https://github.com/$repo.git" main v1.0.0 v1.1.0
fi

# 3. The v1.1.0 release and its two archives, each with one top directory,
# <name>-1.1.0: the .tgz with the submodule's files, the .tar.gz without.
good=${name}_1.1.0_tarball.tgz
plain=$name-1.1.0.tar.gz
if gh release view v1.1.0 --repo "$repo" >/dev/null 2>&1; then
	assets=$(gh release view v1.1.0 --repo "$repo" --json assets --jq '.assets[].name')
	missing=
	for asset in "$good" "$plain"; do
		printf '%s\n' "$assets" | grep -qxF "$asset" || missing="$missing $asset"
	done
	if [ -n "$missing" ]; then
		echo "$repo's release v1.1.0 is there without$missing; left as it is, since this changes no release: delete the release and run again to make it whole"
	else
		echo "$repo's release v1.1.0 is there, with $good and $plain; left as it is"
	fi
elif [ "$DRY" = 1 ]; then
	echo "would make $repo's release v1.1.0, with $good, holding deps/sub's files, and $plain, without them"
else
	git clone -q --recurse-submodules --branch v1.1.0 "https://github.com/$repo.git" "$work/release"
	top=$name-1.1.0
	git_in "$work/release" archive --format=tar --prefix="$top/" v1.1.0 | gzip -n >"$work/$plain"
	mkdir -p "$work/staged"
	git_in "$work/release" archive --format=tar --prefix="$top/" v1.1.0 | tar -x -C "$work/staged"
	rm -rf "$work/staged/$top/deps/sub"
	git -C "$work/release/deps/sub" archive --format=tar --prefix="$top/deps/sub/" HEAD | tar -x -C "$work/staged"
	tar -c -C "$work/staged" "$top" | gzip -n >"$work/$good"
	step gh release create v1.1.0 --repo "$repo" --title v1.1.0 --notes "dockhand's acceptance fixture: $good carries deps/sub's files; $plain doesn't." "$work/$good" "$work/$plain"
fi

# 4. The port, at 1.0.0, fetched with Git and the standard submodule hook.
if [ -f "$portfile" ]; then
	echo "$portfile is there; left as it is"
else
	text=$(cat <<PORTFILE
# -*- coding: utf-8; mode: tcl; tab-width: 4; indent-tabs-mode: nil; c-basic-offset: 4 -*- vim:fenc=utf-8:ft=tcl:et:sw=4:ts=4:sts=4

PortSystem          1.0
PortGroup           github 1.0

github.setup        $owner $name 1.0.0 v
github.tarball_from tarball
revision            0

description         dockhand's acceptance fixture
long_description    A project fetched with Git for its submodule, whose \\
                    release carries an archive holding it, for dockhand's \\
                    source choice.

categories          devel
license             MIT
maintainers         nomaintainer
platforms           any
supported_archs     noarch

fetch.type          git

post-fetch {
    system -W \${worksrcpath} "git submodule update --init"
}

use_configure       no
destroot.args       PREFIX=\${prefix}
PORTFILE
)
	if [ "$DRY" = 1 ]; then
		echo "would write $portfile:"
		printf '%s\n' "$text" | sed 's/^/    /'
	else
		mkdir -p "$(dirname "$portfile")"
		printf '%s\n' "$text" >"$portfile"
		echo "wrote $portfile"
	fi
fi
then_person "full.sh reads ACCEPT_SOURCE_FIXTURE=$repo, and a row copies $portfile into its branch as devel/$name/Portfile"
