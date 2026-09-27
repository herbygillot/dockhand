# 2026-09-27: Xcode images follow MacPorts' buildbots

The person's decision: a release's Xcode image has the Xcode MacPorts' buildbots use, and the table is configurable.

## Why

Setup chose "the newest Xcode the release runs" from the archives it was given, no older than the release's tools generation. That put every Xcode image but Golden Gate's ahead of where MacPorts builds its packages. Rebuilding Tahoe's image with `Xcode_27.xip` in the folder took it from 26.6, the builder's, to 27.0.

| Release | arm64 buildbot | GitHub CI | dockhand's image before |
| --- | --- | --- | --- |
| Monterey | 14.0.1 | — | 14.2 |
| Ventura | 14.3.1 | — | 15.2 |
| Sonoma | 15.4 | 16.2, pinned | 16.2 |
| Sequoia | 16.4 | the runner image's own | 26.3 |
| Tahoe | 26.6 | 26.4, pinned | 26.6, then 27.0 |
| Golden Gate | 27.0 | — | 27.0 |

The buildbot column is the facts table's, from each builder's install-port logs of 2026-09-26. CI's is `xcode-select --switch` in macports-ports' `.github/workflows/bootstrap.sh`. MacPorts itself pins nothing more:
- Base has, per macOS, a minimum, an "ok", and a recommended Xcode, and warns against them (`macports::get_compatible_xcode_versions`);
- 166 Portfiles state a minimum through the `xcodeversion` PortGroup.

## What changed

- **Each release carries its builder's Xcode.** `macos.Release.Xcode` is read from the facts table's arm64 buildbot row (`FactsTable.BuilderXcode`), as `Release.Tools` already is from its generation.
- **Setup takes exactly that version.**
  - `macos.SelectXcode` takes the version asked for, and picks its archive (Apple silicon's before the universal one), never a newer one in its place.
  - Without the archive, setup names the Xcode to download.
  - The Xcode must still run on the release (the upper bound).
  - The floor at the release's tools generation is gone, since the Sonoma builder runs Xcode 15.4 with Command Line Tools 16.2.
- **`providers.tart.xcode` overrides it**, by release name or number: `sonoma = "16.2"`. A name that is no release is refused when setup reads it. `dockhand config` lists each release's Xcode and where it came from.

An image's origin already names its Xcode, so a changed Xcode is a changed identity. The setup protocol is unchanged, re-pinned.

## xcodes downloads what's missing

The person chose [xcodes](https://github.com/XcodesOrg/xcodes) (MIT, active, in MacPorts as `devel/xcodes`) to fetch the archives. Other options were weighed first:
- **Handing the download to the browser:** no credentials in dockhand, but a click per archive, and Apple doesn't document the file URLs.
- **ipsw:** Go, but its sign-in code is in `internal/` packages, which another program can't import.
- **fastlane's xcode-install:** abandoned for xcodes.

**How setup uses it.** When the builder's Xcode is missing from the folder, setup at a terminal asks, then:
- runs `xcodes download <version> --directory <folder>` on the person's terminal, where xcodes asks for the Apple ID and two-factor code itself and keeps the password in the Keychain;
- then finds the archive, which setup takes only once `pkgutil --check-signature` says "signed Apple Software", as it does every Xcode archive (the roadmap's smaller item: the guest's `xip --expand` doesn't check signatures);
- runs setup again.

xcodes names its downloads `Xcode-15.4.0+15F31d.xip`, and setup finds that name as it finds Apple's. A prerelease's name, `Xcode-27.1.0-beta.2+…`, isn't numeric and is never taken. Without a terminal or without xcodes, setup's error names the Xcode to download, and how xcodes would.

**The exception, recorded as a decision.** dockhand calls only xcodes' documented command. xcodes itself signs in through Apple's sign-in and download endpoints, which Apple doesn't document for other programs, so it breaks for a time whenever Apple changes them. Setup then fails loudly, and the archive can still be downloaded by hand.

`xcodes download` has no choice between Apple silicon's and the universal archive. Setup takes either.

**Run live, 2026-09-27.** The person signed in once, downloading 16.4 at a terminal. Then 14.0.1, 14.3.1, and 15.4 were downloaded from a shell with no terminal at all:
- **The sign-in holds.** xcodes keeps the Apple ID in `configuration.json` under `~/Library/Application Support/com.robotsandpencils.xcodes`, the password in the Keychain, and its session under `~/Library/HTTPStorages/xcodes`. No prompt came.
- **Signed out,** it fails at once, exit 1, writing nothing: "Apple ID: Missing username or a password. Please try again."
- **Without a terminal it prints nothing until the end**, then "(1/1) Downloading Xcode 14.0.1+14A400" and "Xcode 14.0.1 has been downloaded to …/Xcode-14.0.1+14A400.xip".
- **A download builds in the system's temporary folder** (`CFNetworkDownload_….tmp`), and the archive is moved into `--directory` only when complete.
- **7.5 GB took about 4 minutes**, without aria2.

Each archive, `Xcode-14.0.1+14A400.xip`, `Xcode-14.3.1+14E300c.xip`, `Xcode-15.4.0+15F31d.xip`, and `Xcode-16.4.0+16F6.xip`, is "signed Apple Software", and setup's selection finds it for its release.

**So setup downloads without a terminal too**, at the person's word. It announces the download, keeps xcodes' output, and on failure shows its last lines with how to sign in once at a terminal. It never reads meaning into what xcodes printed. At a terminal it still asks first, and xcodes signs in there when its session has lapsed.

## Archives to download for parity

Tahoe's (`Xcode_26.6_Apple_silicon.xip`) and Golden Gate's (`Xcode_27.xip`) are in `~/Downloads/xcode_archives`. Monterey, Ventura, Sonoma, and Sequoia need Xcode 14.0.1, 14.3.1, 15.4, and 16.4, from developer.apple.com.

## The images, made again, and the table after them

All twelve images were made again with setup protocol 3 by a delegated run. Each Xcode image has its buildbot's Xcode: Monterey 14.0.1, Ventura 14.3.1, Sonoma 15.4, Sequoia 16.4, Tahoe 26.6, and Golden Gate 27.0. check-24, in Tahoe's Xcode image, recorded the environment's identity ("…; setup 3; …; xcode 26.6; verifier 1"), and kept jq's own archive, which MacPorts now writes: "Creating jq-1.8.2_1.darwin_25.arm64.tbz2".

The facts table was regenerated from the new images and the buildbots' logs (`tools/facts`). The Tart rows for Monterey, Ventura, Sonoma, and Sequoia now name the Xcode their buildbots run: 14.0.1, 14.3.1, 15.4, and 16.4, where they had named 14.2, 15.2, 16.2, and 26.3. Nothing else changed but the rows' sources and dates. Without this, the planner, which prefers a Tart row, would have read ports against the old Xcode, and every check would have reported drift.
