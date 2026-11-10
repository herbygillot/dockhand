#!/bin/bash
# extra-xcode.sh BUCKET PREFIX NAME -- run as root through SSM on an
# instance harvest.sh already set up. Expands the Xcode*.xip under
# s3://BUCKET/installers/PREFIX/ beside the builder's /Applications/Xcode.app
# as /Applications/Xcode_<version>.app, then runs `facts host -name NAME`,
# which probes every Xcode and the Command Line Tools, and copies the probes
# to s3://BUCKET/probes/NAME/.
set -euo pipefail
bucket=$1 prefix=$2 name=$3
work=~ec2-user/facts
export HOME=/var/root PATH=/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin
log() { echo "== $(date -u +%H:%M:%S) $*"; }
cd "$work"
aws s3 cp --quiet "s3://$bucket/tools/facts-darwin-amd64" ./facts && chmod 755 ./facts
xip=$(aws s3 ls "s3://$bucket/installers/$prefix/" | awk '{print $4}' | grep -i '^Xcode.*\.xip$' | head -1)
[ -n "$xip" ] || { echo "no Xcode xip under installers/$prefix/" >&2; exit 1; }
log "Xcode: $xip"
aws s3 cp --quiet "s3://$bucket/installers/$prefix/$xip" "$work/$xip"
rm -rf "$work/extra" && mkdir -p "$work/extra" && cd "$work/extra"
log "expanding $xip"
xip --expand "$work/$xip" >/dev/null
rm -f "$work/$xip"
app=$(ls -d "$work"/extra/Xcode*.app | head -1)
version=$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$app/Contents/Info.plist")
target=/Applications/Xcode_$version.app
rm -rf "$target" && mv "$app" "$target"
cd "$work"
xcode-select --switch "$target/Contents/Developer"
xcodebuild -license accept
xcodebuild -runFirstLaunch >/dev/null 2>&1 || true
xcodebuild -version
xcode-select --switch /Applications/Xcode.app/Contents/Developer
chown -R ec2-user "$work"
log "probing"
rm -rf "$work/probes-$name"
sudo -u ec2-user -H "$work/facts" host -name "$name" -out "$work/probes-$name"
aws s3 cp --quiet --recursive "$work/probes-$name" "s3://$bucket/probes/$name/"
log "done: s3://$bucket/probes/$name/"
