# 2026-09-26: `providers setup tart` and `providers`

v3 makes its own Tart images now. `dockhand providers setup tart
[release]` puts a command over the provisioner that v2's `setup` used
(`internal/tart/provision`), and `dockhand providers` says which providers
are ready (Design v3 §6.1 and its command table, where v2's `setup`
becomes `init` and `providers setup tart`).

## What it does

- **`providers setup tart [release]`** makes `dockhand-base-<release>` in
  dockhand's Tart home. With no release it's this Mac's.
  - When the image exists, it is checked in a disposable clone and left as
    it is.
  - `--check` only checks, and `--rebuild` makes a replacement.
  - `--macports-version` picks the MacPorts it installs (2.12.6 unless
    given).
  - Before making an image, it names the cost: up to 60 GB of disk. An
    image that exists, or has a golden copy to restore from, costs only a
    check, so no cost is named then.
  - When this Mac's MacPorts differs from the image's, it says so: ports
    are read with one and built with the other.
- **`providers`** shows a line for each provider:
  - tart: its releases with images, or the setup command and its cost;
  - github: whether there's a login or token;
  - command: the script, when the configuration file names one.

  `init` shows the same lines under "Providers", as in the design's first
  run.
- **The provider owns it.** `tart.Provider` gained `Status` and `Setup`,
  so the command layer still imports only `internal/provider/tart`, not the
  provisioner. The host's MacPorts version is read with
  `installation.ParseVersion`, split out of `Inspect` for this.

## What was left out, on purpose

- **`--xcode`.** The provisioner can make an Xcode image, but no v3 check
  uses one until choosing an image by a port's modelled `use_xcode` lands
  (decisions 6 and 7). Offering it now would spend 30 GB on an image
  nothing reads. `providers` doesn't list Xcode images for the same
  reason. `Status` reports them for when it does.
- **v2's `--image`, `--source`, and `--capacity`.** A check reads only
  `dockhand-base-<release>`. The source is Cirrus Labs' image for the
  release. Capacity is `[providers.tart] capacity` in the configuration
  file.
- **`prefix`.** The design's third provider line waits for the prefix
  provider itself.

## Measured, not assumed

- **The disk figure.** Tahoe's image is 29 GB, and the vanilla image it
  starts from is 28 GB.
- **The golden copy shares the image's blocks.** Cloning the 19 GB Ventura
  image took no measurable time and 28 KB of disk. The throwaway clone was
  deleted, and the image was only read. So the design's illustrative
  "45 GB" became "up to 60 GB".

## Tests

- **The provider:**
  - `Status` lists releases with base and Xcode images, oldest first,
    ignoring clones and golden copies;
  - `Setup` refuses `--check` with `--rebuild`, and an unknown release;
  - it names the cost exactly when an image will be made: a missing
    image, or `--rebuild`, but not an existing image, one with a golden
    copy, or `--check`.
- **The command line**, over a stand-in for the images:
  - `providers` without Tart, with no images, with some, with this Mac's,
    and with Tart failing to list;
  - a command provider's line;
  - `setup tart` making, checking, and failing, with its options passed
    through;
  - init's Providers lines.

## Proven on the Mac

- **`dockhand providers`**, run on this Mac:
  - tart ✓, images for macOS 12, 13, 14, 15, and 26;
  - github ✓, with the Keychain login.
- **`dockhand providers setup tart --check`** checked `dockhand-base-tahoe`
  in a disposable clone, which was deleted afterwards:
  - "Ready: dockhand-base-tahoe, macOS 26 (Tahoe) with Command Line Tools
    26.6 and MacPorts 2.12.6";
  - no MacPorts warning, since the host's version matches.

  No image was made or replaced.
