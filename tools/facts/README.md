# The toolchain facts table

`tools/facts` harvests and regenerates `internal/macos/facts.json`: what
each macOS release's developer tools are, as MacPorts Base sees them, per
architecture and profile (decisions 9–13 of the
[contracts direction](../../docs/reviews/2026-09-23-contracts-direction.md)).
A modelled context answers Base's toolchain questions from the table
rather than from the Mac dockhand runs on ([oracle](../../docs/oracle.md),
phase 5). The table is regenerated, never edited by hand.

```sh
go build -o /tmp/facts ./tools/facts
dir=~/.dockhand/surveys/$(date +%F)-toolchain-facts

# MacPorts' buildbots: the header Base prints in each builder's recent
# install-port log, for every release and architecture they build.
/tmp/facts buildbot -out $dir/buildbot.json

# dockhand's Tart images: each base and Xcode image probed through a clone,
# two at a time, the clone deleted after. The images are only read.
/tmp/facts tart -out $dir/tart

# The table, from both.
/tmp/facts generate -tart $dir/tart -buildbot $dir/buildbot.json -out internal/macos/facts.json
```

## The sources

- **Tart images** (`probe.tcl`): arm64, Darwin 21 and later, in both
  profiles, the Command Line Tools alone (`dockhand-base-*`) and Xcode
  (`dockhand-xcode-*`). The probe asks Base itself, in the parent and in a
  throwaway port's worker, over dockhand's SSH channel, under the image's
  own `port-tclsh`.
- **Buildbots**: every release and architecture MacPorts builds, from
  Darwin 10. A log gives Base's view of the macOS release, Xcode, the
  tools, and the SDK. It doesn't give clang's build number, the developer
  directory, or the SDKs installed. The buildbots carry full Xcode, so
  their rows are the Xcode profile.

Where both describe a release, architecture, and profile, the Tart image
wins (`macos.FactsTable.Lookup`) and the buildbot's row stays as a
cross-check. Darwin 8 and 9 have no builder, and Base's own tables are
not read yet.

## The tools generation setup installs

The table also holds each release's Command Line Tools generation, the
major version setup installs (decision 13): what MacPorts' arm64 builder
for the release runs, since setup builds arm64 images. `macos.Release`'s
`Tools` is read from it. Where MacPorts' GitHub CI pins Xcode, check by
hand when regenerating that the pin is of the same generation. Look for
`xcode-select --switch` in `.github/workflows/bootstrap.sh` in
macports-ports; dockhand doesn't parse it. On 2026-09-26, at
`abd9fff84df`, CI pinned Xcode 16.2 on Darwin 23 and 26.4 on Darwin 25,
generations 16 and 26, as the builders run. If a pin ever disagrees, it
should win, which the generator can't yet express: that is when to teach it.

## Staleness

The table is shared, so its rows can't name a person's images by digest.
Each records its source image or builder and build, its date, and the
MacPorts version that read it. A table drifts when Apple ships new tools
or setup rebuilds an image, which is when to harvest again. Decision 10's
drift report, once verification probes its guests, compares a guest's
facts with its row.
