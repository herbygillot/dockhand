# 2026-09-26: setup's tools generation comes from the facts table

Decision 13 of the
[contracts direction](../reviews/2026-09-23-contracts-direction.md):
setup installs a pinned Command Line Tools generation per release, and
the facts table holds it. Setup already installed only the generation
`macos.Release.Tools` names, since step 2 rebuilt the Tahoe images on
the 26 tools. The generation itself, though, was written by hand in the
Darwin table, and the [facts table](2026-09-26-facts-table.md) didn't
hold it.

## What changed

- **The table holds each release's generation.** `tools/facts generate`
  derives it from the buildbot harvest: the major version of the tools
  MacPorts' arm64 builder for the release runs, since setup builds arm64
  images. It records the builder and build it came from. The Intel
  builder can differ: on macOS 15 it runs CLT 26.0 against arm64's
  16.4.
- **`macos.Release.Tools` is read from the table**, not written in
  `release.go`. The derived generations are the ones written there by
  hand on 2026-09-23: 14 for Monterey and Ventura, 16 for Sonoma and
  Sequoia, 26 for Tahoe, and 27 for Golden Gate. The harvest adds 13 for
  Big Sur, which setup doesn't build.
- **MacPorts' CI pins, checked by hand.** At `abd9fff84df`,
  `.github/workflows/bootstrap.sh` switches to Xcode 16.2 on Darwin 23 and
  26.4 on Darwin 25. Those are generations 16 and 26, as the builders run.
  dockhand doesn't parse the script. The
  [README](../../tools/facts/README.md) says how to check it when the
  table is regenerated.

## Tests

- Every release setup builds has a generation.
- Every image in the table carries its release's generation. An image
  setup didn't pin, as the Tahoe images weren't before 2026-09-25, fails
  the test once the table is harvested from it.
- The generator takes the arm64 builder's tools, not the Intel one's or
  an image's.
- `Check` refuses a release given two generations, or one without its
  source.
