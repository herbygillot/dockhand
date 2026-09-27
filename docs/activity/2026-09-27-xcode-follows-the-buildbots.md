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
- takes the archive only once `pkgutil --check-signature` says "signed Apple Software";
- runs setup again.

xcodes names its downloads `Xcode-15.4.0+15F31d.xip`, and setup finds that name as it finds Apple's. A prerelease's name, `Xcode-27.1.0-beta.2+…`, isn't numeric and is never taken. Without a terminal or without xcodes, setup's error names the Xcode to download, and how xcodes would.

**The exception, recorded as a decision.** dockhand calls only xcodes' documented command. xcodes itself signs in through Apple's sign-in and download endpoints, which Apple doesn't document for other programs, so it breaks for a time whenever Apple changes them. Setup then fails loudly, and the archive can still be downloaded by hand.

`xcodes download` has no choice between Apple silicon's and the universal archive. Setup takes either.

## Archives to download for parity

Tahoe's (`Xcode_26.6_Apple_silicon.xip`) and Golden Gate's (`Xcode_27.xip`) are in `~/Downloads/xcode_archives`. Monterey, Ventura, Sonoma, and Sequoia need Xcode 14.0.1, 14.3.1, 15.4, and 16.4, from developer.apple.com.
