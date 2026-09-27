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
