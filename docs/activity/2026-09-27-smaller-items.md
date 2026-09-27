# 2026-09-27: the roadmap's smaller items

Taken between items, each its own commit.

## Xcode archives are Apple's

Setup refuses an Xcode archive that `pkgutil --check-signature` doesn't call "signed Apple Software", before it makes or checks anything (`Provisioner.Signature`). The guest's `xip --expand` doesn't check signatures. The check on xcodes' downloads alone went with it ([note](2026-09-27-xcode-follows-the-buildbots.md)).

## One image descriptor

A release's images were named in four places: `tart/release.go`'s defaults, provisioning's `goldenName`, the provider's `baseImage`, `xcodeImage`, and `goldenImage`, and `tools/facts`' prefix match. They disagreed at the edge. For an image under another name, provisioning's golden copy was `<name>-golden`, and the provider's was `dockhand-golden-<rest>`.

`tart.Prepared{Release, Profile}` now names them all:
- `Name`: `dockhand-base-tahoe` or `dockhand-xcode-tahoe`;
- `Golden`: `dockhand-golden-tahoe` or `dockhand-golden-xcode-tahoe`;
- `Source`: the vanilla image;
- `ParsePrepared` reads a name back;
- `GoldenName` gives any image's golden copy, `<name>-golden` for one under another name.

Setup's check that an image holds what its name says reads the name with `ParsePrepared`, rather than by prefix. The profile is the facts table's (`macos.ProfileTools`, `macos.ProfileXcode`). The names themselves are unchanged, so existing images stand.

## What a Tart guest reported, kept whole

The architecture review of 2026-09-27 found three reductions where Tart's results were converted.

**Why a target stopped** was only a progress line. It is now the result's `Detail` (schema 17): MacPorts' last errors for a failed target, or the changed dependency that blocked one. `dockhand logs` shows it after the outcome ("harbor-cli failed at install: …"), and `--json` as `detail`. Schema 11 had dropped schema 10's detail, which only said what the plan now says, an unmet need; this one is the build's own.

**The guest's MacPorts and developer directory** were read and dropped, since `model.Observed` had no place for them. It now has one. The guest records `port version`'s own words, "Version: 2.12.6", which the provider reads with `installation.ParseVersion`. Tested on names it: "Xcode 26.6 17F42 · MacPorts 2.12.6 · tart: …".

**Tested on stood the latest report for every run.** `Evidence.Observed` took the newest nonempty report, even for results from an earlier check's run with other tools. `Evidence.Observations` now groups an environment's runs by what they reported:
- one line where they agree, as before;
- one each, naming its runs, where they don't;
- a run that reported nothing joins the one report where there is one, and otherwise stands as its own.

## `outdated` shows its progress

A large `--mine` was minutes of silence: 1,076 ports took three. `outdated.Service` now tells an optional `Progress` how many ports are looked up: once before the first, and after each, in order, never two calls at once. The engine's `OutdatedRequest` carries it, and `outdated` and `update --outdated` draw one line on standard error, "Looking up each port's newest release: 312 of 1,076", redrawn in place and cleared at the end. That happens only when standard error is a terminal, and never with `--json` (`Streams.errTerminal`). Serve's daily look passes none.
