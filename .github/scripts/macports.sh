#!/usr/bin/env bash
# Installs the MacPorts release MacPorts points its users at, as ci.yml's
# comment describes, for this runner's macOS and architecture.
set -euo pipefail
release=$(curl -fsSL https://raw.githubusercontent.com/macports/macports-base/master/config/RELEASE_URL)
version=${release##*/v}
if ! printf '%s\n' "$version" | grep -Eqx '[0-9]+\.[0-9]+\.[0-9]+'; then echo "MacPorts' RELEASE_URL names no release: $release" >&2; exit 1; fi
major=$(sw_vers -productVersion | cut -d. -f1)
base=https://github.com/macports/macports-base/releases/download/v$version
curl -fsSLO "$base/MacPorts-$version.chk.txt"
package=$(sed -n "s/^SHA2-256(\(MacPorts-$version-$major-[A-Za-z]*\.pkg\))= .*/\1/p" "MacPorts-$version.chk.txt")
if [ -z "$package" ]; then echo "MacPorts $version has no package for macOS $major" >&2; exit 1; fi
sum=$(sed -n "s/^SHA2-256($package)= //p" "MacPorts-$version.chk.txt")
curl -fsSLO "$base/$package"
echo "$sum  $package" | shasum -a 256 -c -
sudo mkdir -p /opt/local/etc/macports "$RUNNER_TEMP/ports"
echo "file://$RUNNER_TEMP/ports [default,nosync]" | sudo tee /opt/local/etc/macports/sources.conf
sudo installer -pkg "$package" -target /
/opt/local/bin/port version
echo "DOCKHAND_TEST_MACPORTS_TCLSH=/opt/local/bin/port-tclsh" >> "$GITHUB_ENV"
