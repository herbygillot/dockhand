#!/bin/bash
# harvest.sh BUCKET RELEASE -- run as root on an EC2 Intel Mac (mac1.metal)
# through SSM Run Command. Installs MacPorts 2.12.7 (CI's pin), the
# release builder's Xcode as /Applications/Xcode.app and its Command Line
# Tools, from s3://BUCKET/installers/RELEASE/ (one Xcode*.xip, one
# Command_Line_Tools*.dmg, or a NO_CLT marker where the builder has none),
# then runs tools/facts' `facts host` as ec2-user (macOS 12 and later;
# below, where a Go 1.27 binary can't load, probe.tcl directly with the
# same document around it) and copies the probes to
# s3://BUCKET/probes/aws-mac1-RELEASE/.
set -euo pipefail
bucket=$1 release=$2
name=aws-mac1-$release
work=~ec2-user/facts
export HOME=/var/root PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin
mkdir -p "$work" && cd "$work"
log() { echo "== $(date -u +%H:%M:%S) $*"; }
# GitHub's release downloads sometimes answer 504; macOS 10.15's curl has
# no --retry-all-errors, so retry here.
fetch() { for try in 1 2 3 4 5 6; do curl -fsSLO "$1" && return; sleep $((try * 20)); done; return 1; }

log "macOS $(sw_vers -productVersion) $(uname -m)"
# Grow the APFS container to the whole EBS volume (launched at 200 GB), as
# AWS documents for its Mac AMIs; an Xcode 13 xip expands to about 35 GB.
pdisk=$(diskutil list physical external | head -n1 | cut -d' ' -f1)
container=$(diskutil list physical external | grep Apple_APFS | tr -s ' ' | cut -d' ' -f8)
yes | diskutil repairDisk "$pdisk" >/dev/null 2>&1 || true
diskutil apfs resizeContainer "$container" 0 >/dev/null 2>&1 || true
df -h / | tail -1
aws s3 cp --quiet "s3://$bucket/tools/facts-darwin-amd64" ./facts && chmod 755 ./facts
aws s3 cp --quiet "s3://$bucket/tools/probe.tcl" ./probe.tcl

# MacPorts, as the repo's .github/scripts/macports.sh installs it, at CI's pin.
version=2.12.7
major=$(sw_vers -productVersion | cut -d. -f1)
[ "$major" = 10 ] && major=$(sw_vers -productVersion | cut -d. -f1-2)
base=https://github.com/macports/macports-base/releases/download/v$version
fetch "$base/MacPorts-$version.chk.txt"
package=$(sed -n "s/^SHA2-256(\(MacPorts-$version-$major-[A-Za-z]*\.pkg\))= .*/\1/p" "MacPorts-$version.chk.txt")
[ -n "$package" ] || { echo "MacPorts $version has no package for macOS $major" >&2; exit 1; }
fetch "$base/$package"
echo "$(sed -n "s/^SHA2-256($package)= //p" "MacPorts-$version.chk.txt")  $package" | shasum -a 256 -c -
mkdir -p /opt/local/etc/macports "$work/ports"
echo "file://$work/ports [default,nosync]" > /opt/local/etc/macports/sources.conf
log "installing $package"
installer -pkg "$package" -target / >/dev/null
/opt/local/bin/port version

# The builder's Command Line Tools, replacing whatever the AMI carries, so
# Base's xcodecltversion (pkgutil's receipt) is the builder's.
listing=$(aws s3 ls "s3://$bucket/installers/$release/" | awk '{print $4}')
dmg=$(echo "$listing" | grep -i '^Command_Line_Tools.*\.dmg$' | head -1 || true)
rm -rf /Library/Developer/CommandLineTools
for receipt in $(pkgutil --pkgs | grep -E '^com\.apple\.pkg\.(CLTools_|DevSDK|DeveloperToolsCLI)' || true); do pkgutil --forget "$receipt" >/dev/null; done
if [ -n "$dmg" ]; then
  log "Command Line Tools: $dmg"
  aws s3 cp --quiet "s3://$bucket/installers/$release/$dmg" "$work/$dmg"
  mount=$(hdiutil attach -nobrowse -readonly "$work/$dmg" | awk -F'\t' '/\/Volumes\//{print $NF}' | tail -1)
  installer -pkg "$(ls -d "$mount"/*.pkg | head -1)" -target / >/dev/null
  hdiutil detach "$mount" >/dev/null
elif echo "$listing" | grep -qx NO_CLT; then
  log "no Command Line Tools, as on the builder"
else
  echo "no Command Line Tools dmg or NO_CLT marker under installers/$release/" >&2; exit 1
fi
pkgutil --pkg-info=com.apple.pkg.CLTools_Executables 2>/dev/null | grep '^version' || echo 'tools: none'

# The builder's Xcode, at /Applications/Xcode.app as on the builders. Any
# other Xcode the AMI carries is set aside so the probe sees one.
xip=$(aws s3 ls "s3://$bucket/installers/$release/" | awk '{print $4}' | grep -i '^Xcode.*\.xip$' | head -1)
[ -n "$xip" ] || { echo "no Xcode xip under installers/$release/" >&2; exit 1; }
log "Xcode: $xip"
aws s3 cp --quiet "s3://$bucket/installers/$release/$xip" "$work/$xip"
mkdir -p /Applications/.set-aside
for app in /Applications/Xcode*.app; do [ -e "$app" ] && mv "$app" /Applications/.set-aside/; done
mkdir -p "$work/xip" && cd "$work/xip"
log "expanding $xip"
xip --expand "$work/$xip" >/dev/null
rm -f "$work/$xip"
mv "$(ls -d "$work"/xip/Xcode*.app | head -1)" /Applications/Xcode.app
cd "$work"
xcode-select --switch /Applications/Xcode.app/Contents/Developer
xcodebuild -license accept
xcodebuild -runFirstLaunch >/dev/null 2>&1 || true
xcodebuild -version

# The probe, as ec2-user (an admin with passwordless sudo, as `facts
# host` needs), Xcode selected then the tools.
chown -R ec2-user "$work"
log "probing"
if [ "${major%%.*}" -ge 12 ]; then
  sudo -u ec2-user -H "$work/facts" host -name "$name" -out "$work/probes"
else
  # facts host's document, written here: Xcode selected, then the tools.
  mkdir -p "$work/probes" && chown ec2-user "$work/probes"
  for developer in /Applications/Xcode.app/Contents/Developer /Library/Developer/CommandLineTools; do
    [ -d "$developer" ] || continue
    profile=xcode slug=Xcode
    [ "$developer" = /Library/Developer/CommandLineTools ] && profile=clt slug=clt
    xcode-select --switch "$developer"
    rm -rf /opt/local/var/macports/cache
    out="$work/probe-$slug.json"
    output=$(sudo -u ec2-user -H /opt/local/bin/port-tclsh "$work/probe.tcl" "$out" 2>&1 || true)
    {
      printf '{\n  "image": "%s", "slug": "%s", "profile": "%s", "source": "host",\n' "$name" "$slug" "$profile"
      printf '  "developer": "%s", "date": "%s",\n' "$developer" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      printf '  "host": {"macos": "%s"}, "probe_output": %s, "driver_errors": [],\n  "facts": ' "$(sw_vers -productVersion)" "$(printf %s "$output" | /usr/bin/python -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null || echo '""')"
      cat "$out"
      printf '}\n'
    } > "$work/probes/$name-$slug.json"
  done
  xcode-select --switch /Applications/Xcode.app/Contents/Developer
fi
aws s3 cp --quiet --recursive "$work/probes" "s3://$bucket/probes/$name/"
log "done: s3://$bucket/probes/$name/"
