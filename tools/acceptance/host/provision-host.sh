#!/usr/bin/env bash
# provision-host.sh sets up the test host, once, as its admin driver (the
# project's plan/prime-time-environment.md). It's idempotent, so it can
# run again after an OS update. Without --run, it only says what it would
# do, and changes nothing.
#
#   tools/acceptance/host/provision-host.sh [--run]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
# shellcheck source=lib.sh
. "$here/lib.sh"

# 1. The hardware, macOS, disk, and memory the full stage needs.
short=""
[ "$(uname -m)" = arm64 ] || short="$short Apple silicon (this is $(uname -m));"
major=$(sw_vers -productVersion | cut -d. -f1)
[ "$major" -ge 27 ] || short="$short macOS 27 (this is $(sw_vers -productVersion));"
memory_gb=$(($(sysctl -n hw.memsize) / 1000000000))
[ "$memory_gb" -ge 32 ] || short="$short 32 GB of memory ($memory_gb GB);"
free_gb=$(df -g / | awk 'NR == 2 {print $4}')
[ "$free_gb" -ge 800 ] || short="$short 800 GB free for images ($free_gb GB);"
if [ -n "$short" ]; then
	echo "provision-host.sh: this Mac is short of:$short" >&2
	[ "$DRY" = 1 ] || exit 1
fi
echo "Hardware: $(uname -m), macOS $(sw_vers -productVersion), $memory_gb GB, $free_gb GB free"

# 2. MacPorts, which the tests use; installing it is a person's, with sudo.
if [ -x /opt/local/bin/port ]; then
	echo "MacPorts: $(/opt/local/bin/port version)"
else
	human "install MacPorts from macports.org's package for this macOS, then run this again"
fi

# 3. The tools: Tart 2.39 or newer, gh, and the dependency generators.
for tool in tart gh go2port cargo2port; do
	command -v "$tool" >/dev/null || step sudo /opt/local/bin/port -N install "$tool"
done
if command -v tart >/dev/null; then
	version=$(tart --version | awk '{print $NF}')
	case "$(printf '%s\n2.39.0\n' "$version" | sort -V | head -1)" in 2.39.0) ;; *) human "Tart $version is older than 2.39, which Golden Gate's images need: sudo port upgrade tart" ;; esac
fi

# 4. A mirror of macports-ports, its quick-stage branch pinned.
step mkdir -p "$HOST_ROOT"
if [ ! -d "$HOST_ROOT/ports-mirror.git" ]; then
	step git clone --mirror https://github.com/macports/macports-ports.git "$HOST_ROOT/ports-mirror.git"
else
	step git -C "$HOST_ROOT/ports-mirror.git" fetch --prune
fi

# 5. The Xcode a person staged, checked. Only A12 uses one, this Mac's
# release's, for an Xcode image; without one A12 isn't run, so setup goes
# on without waiting (the Prime-time thread, 2026-10-04: no other row
# uses an Xcode image).
if ls "$HOST_ROOT"/xcode/*.xip >/dev/null 2>&1; then
	for xip in "$HOST_ROOT"/xcode/*.xip; do
		step pkgutil --check-signature "$xip"
	done
else
	echo "optional: for A12's Xcode image, download this Mac's release's Xcode with your Apple ID into $HOST_ROOT/xcode; without it A12 isn't run"
fi

# 6. The overlay the candidate's Portfile goes in, as a source MacPorts reads.
step mkdir -p "$HOST_ROOT/ports/sysutils/dockhand"
if ! grep -qF "file://$HOST_ROOT/ports" /opt/local/etc/macports/sources.conf 2>/dev/null; then
	human "add 'file://$HOST_ROOT/ports' above the default line of /opt/local/etc/macports/sources.conf, then run portindex in $HOST_ROOT/ports"
fi

# 7. The marker that lets reset-user.sh delete the test user, which a
# person writes once they've chosen this Mac.
[ -f "$HOST_MARKER" ] || human "if this Mac is the test host, write $HOST_MARKER (sudo touch), which reset-user.sh requires"
echo "provision-host.sh: done$([ "$DRY" = 1 ] && echo ', as a dry run: nothing was changed')"
