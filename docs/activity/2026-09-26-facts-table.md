# 2026-09-26: the toolchain facts table and its harvesters

Roadmap step 6's first part, decisions 9–13 of the
[contracts direction](../reviews/2026-09-23-contracts-direction.md). It
records what each macOS release's developer tools are, as MacPorts Base
sees them. The oracle's phase 5 answers modelled contexts' toolchain
questions from it rather than from the Mac dockhand runs on.

## What was added

- **The table**, `internal/macos/facts.json`, embedded and read through
  `macos.Table()`, beside the Darwin table where decision 9 put it. Each
  row is keyed by Darwin, architecture, and profile. The profile is
  `clt`, the Command Line Tools alone as in dockhand's base images, or
  `xcode`, full Xcode as in its Xcode images and the buildbots. A row
  records Base's view:
  - `xcodeversion` and `xcodecltversion`;
  - the developer directory;
  - the SDKs the tools carry, and Base's `macosx_sdk_version`;
  - the build number of the tools' clang, which compiler selection
    compares;
  - whether `/usr/lib/libxcselect.dylib` exists;
  - its source: image or builder and build, date, and MacPorts version.

  `Lookup` prefers a Tart image to a buildbot, and `Check` refuses a row
  without its key, its versions, or its source. The table is generated,
  never edited.
- **`tools/facts`**, a developer tool beside `tools/survey`
  ([README](../../tools/facts/README.md)):
  - **`buildbot`** reads the debug header Base prints in each builder's
    recent `install-port` log: macOS and Darwin, MacPorts, Xcode and the
    tools, and the SDK. It accepts all three names Base has given the
    release: Mac OS X, OS X, and macOS.
  - **`tart`** probes dockhand's base and Xcode images, two at a time,
    each through a clone it boots, reaches over dockhand's SSH channel
    (the clone presents its image's recorded host keys), and deletes
    after. The images are only read. The probe is the 2026-09-23
    `probe.tcl`, now checked in, which asks Base itself in the parent and
    in a throwaway port's worker.
  - **`generate`** turns both into the table, keeping the newest harvest
    of each row.

## The harvest

`~/.dockhand/surveys/2026-09-26-toolchain-facts/`: all ten images and all
24 ports builders, 34 rows. The Tart rows match the 09-23 and 09-25
probes, with Tahoe now on the 26 tools. The buildbots give every release
and architecture from Darwin 10 (Mac OS X 10.6, i386 and x86_64) to 27.

What the buildbots don't give: clang's build number, the developer
directory, and the SDKs installed. Their logs print Base's header, but
no compiler's version string. Phase 5 decides how a modelled context
answers where a row is silent. For instance, the tools package on an
x86_64 builder is the one probed on an arm64 image when the versions
match, as for Darwin 22 and 25.

## Two things the harvest showed

- **x86_64 is buildbot-only.** Tart boots arm64 macOS only, so darwin 25
  x86_64, the context preparation models most (about 25,000 ports in a
  survey), has only a buildbot row, in the Xcode profile.
- **A release's Intel and arm64 builders can run different tools:** CLT
  26.0 on macOS 15 x86_64 against 16.4 on arm64, and 15.3 against 16.2 on
  macOS 14. A row is per architecture for that reason.

## A correction to the scope

`docs/oracle.md` had each row carry its image's digest, with a changed
image making its row stale. The table is checked in and shared, so a row
can't name a person's images by digest. v2's digest also hashes the
whole disk, which takes minutes. Staleness is found by drift instead, as
decision 10 has it: a guest's probed facts are compared with its row. The
scope now says so.
