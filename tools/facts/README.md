# The toolchain facts table

`tools/facts` harvests and regenerates `internal/macos/facts.json`: what
each macOS release's developer tools are, as MacPorts Base sees them, per
architecture and profile ([decisions](../../docs/decisions.md) 9 to 13).
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

# GitHub's macOS runners: the latest run of the Toolchain facts workflow
# (.github/workflows/facts-runners.yml), one artifact for each image.
gh run download --repo herbygillot/dockhand --name facts-macos-15 --dir $dir/runner \
  $(gh run list --repo herbygillot/dockhand --workflow facts-runners.yml --status success --limit 1 --json databaseId -q '.[0].databaseId')
# ...and likewise facts-macos-15-intel, facts-macos-26, facts-macos-26-intel.
# With no --name, gh downloads every artifact of the run at once, each into
# its own directory under $dir/runner; generate reads only the *.json directly
# in a directory, so pass each of those as its own -runner.

# Hosts: Macs probed once, kept in tools/facts/probes, one directory each.
# Installers: readings of Xcode and Command Line Tools installers, kept in
# tools/facts/installers.
# The table, from all five.
/tmp/facts generate -tart $dir/tart -runner $dir/runner -host tools/facts/probes/aws-mac1 \
  -installer tools/facts/installers/legacy-2026-11-10.json \
  -installer tools/facts/installers/xcode-archives-2026-11-10.json \
  -buildbot $dir/buildbot.json -out internal/macos/facts.json
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

- **GitHub's runners** (`probe.tcl`, `facts runner`): the macOS images
  MacPorts' CI starts from, Intel and arm64, macOS 15 and 26, with MacPorts
  installed as CI installs it, Homebrew set aside. A weekly workflow probes
  each image once with every Xcode it carries selected and once with the
  Command Line Tools selected, Xcode still on disk, which Base reads as no
  Xcode. A row's source names the image, the run, and what was selected.
  The table keeps a runner's row only where it has the toolchain the
  release and architecture's buildbot runs, its Xcode and its tools, the
  tools by version: a build of the same release that differs only in the
  trailing build stamp, 26.0.0.0.1.1756965055 for the builder's
  26.0.0.0.1.1757719676, carries the same clang and SDKs. `generate`
  reports the others, which wait for the table's lookup to choose among
  toolchains. On 2026-11-05 the Intel and arm64 runners gave
  the same SDKs, Xcode SDKs, and clang as the Tart images for the same
  Xcode and tools, field for field: these facts are the version's, not
  the Mac's.

- **Hosts** (`probe.tcl`, `facts host`): any other Mac probed once the way
  a runner is, every Xcode then the Command Line Tools, kept by the same
  rule. Its probes can't be made again, so they are committed under
  [probes](probes), one directory per host, and `generate -host` reads them
  every time. [aws-mac1](probes/aws-mac1) is an AWS EC2 Intel Mac
  (mac1.metal) on 2026-11-09: macOS 10.14 to 15, each with its x86_64
  builder's Xcode and tools (Catalina's builder has no tools), MacPorts
  2.12.7, and on macOS 13 Xcode 15.2 too, the newest Xcode it runs, which
  `generate` reports rather than keeps until the lookup chooses among
  Xcodes. Below macOS 12, where `facts` can't run, the harvest ran
  `probe.tcl` itself and wrote the same document. [aws](aws) holds the
  scripts that made them: `driver.sh` drives one Dedicated Host over the
  EC2, SSM, and S3 APIs (allocate, launch, harvest, finish, release), and
  `harvest.sh` installs MacPorts, the builder's tools and Xcode from the
  person's own S3 bucket, then probes; `extra-xcode.sh` adds another Xcode
  beside it. A host is billed for 24 hours at least, and AWS scrubs it for
  about an hour between instances.

- **Installers** (`generate -installer`): Xcode and Command Line Tools
  installers, .dmg and .xip, read without installing them or running
  anything in them: each Xcode's SDKs (`xcode_sdks`), the tools' SDKs,
  each one's clang build, read from the clang binary's version string, and
  the developer directory, /Developer for Xcode 3. They give no live Base
  view, so an installer's row stands on a buildbot's: the builder's
  release, macOS, Xcode, tools, and SDK, with the facts of the installers
  of that Xcode and those tools, the tools by version as a runner's are.
  The builder's tools give the clang Base reads, Xcode's where it runs
  none. [installers](installers) holds two readings made on 2026-11-10 on
  the person's Mac, from its archive of Apple's downloads:
  `legacy-2026-11-10.json`, Xcode 3.2.6 to 9.4.1 and the tools 6.2 to
  9.4.1, the builders' from Darwin 10 to 17, and
  `xcode-archives-2026-11-10.json`, Xcode 10.3 to 27 and the tools 10.3 to
  26, which gave the same SDKs and clang as the hosts' and the Tart images'
  probes for every version both have. The tools 6.2 and 7.2 install no SDK,
  their headers going to /usr/include. Each installer's own file, with its
  packages and the installer's OS checks, stays with the archive.

Where more than one describes a release, architecture, and profile, the
Tart image wins, then the runner, then the host, then the installers
(`macos.FactsTable.Lookup`), and the buildbot's row stays as a cross-check. Darwin 8 and 9 have no builder, and Base's own tables are
not read yet.

## The Xcode setup installs

Each release's Xcode image installs the Xcode its arm64 builder runs, the
buildbot row's Xcode (`macos.FactsTable.BuilderXcode`), unless
`providers.tart.xcode` names another. The Tart rows don't count for this,
since they describe dockhand's own images. Regenerating the table moves an
Xcode image to whatever Xcode the builder has been upgraded to, the next
time the image is made.

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
