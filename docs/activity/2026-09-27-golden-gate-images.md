# 2026-09-27: Golden Gate images, on Tart 2.39.0

Golden Gate's images, macOS 27, have ASIF disks. Dockhand declined them because Tart couldn't list its VMs while one ran (openai/tart#1344), which is how setup was once left unable to stop a guest ([2026-09-24](2026-09-24-tart-official-interfaces.md)). Tart 2.39.0 fixes the listing, and the person asked whether that is enough to verify on Golden Gate. It was, with one change to how the disk is prepared. A check now passes on it.

## Experiments

They ran on the Mac, in dockhand's Tart home, on clones of `ghcr.io/cirruslabs/macos-golden-gate-vanilla:latest`, pulled there for this: 30.9 GB compressed, a 50 GB disk, 35 GB on the host. They used clones named `exp-gg-*`, deleted afterwards, and changed no image.

- **Listing while it runs.**
  - `tart list` and `tart get` both answer while the ASIF VM runs.
  - The disk's capacity reads `null`. Dockhand doesn't read it: `Images` and `Get` take only Name, Source, Running, State, and DiskFormat.
  - `DiskFormat` still says `asif`.
  - The fix is `try?` on the capacity lookup, #1349. The tag that first contains it is 2.39.0, so 2.39.0 is the minimum, not 2.38.0.
- **Listing while it starts.** On 2.37.0, a listing in an ASIF VM's first seconds failed ten of eleven starts. On 2.39.0, eleven of eleven starts succeeded, each listed every second through its first ten. No run or listing logged an error.
- **The layout** is a standalone `disk.img` in ASIF format on this Tahoe host, not the stacked `overlay.asif` a macOS 27 host could make. The path setup and the Tart provider use holds.
- **The guest** was macOS 27.0, build 26A428, Darwin 27.0.0. Its 50 GB disk held a 44.1 GB container, then a 5.4 GB recovery partition, with 10 GB free. Passwordless sudo worked, and there was no Tart guest agent, which setup installs.
- **Growing the disk.** For an ASIF disk, `tart set --disk-size 100` runs `diskutil image resize`. Seen from the guest:
  - the disk grew to 100 GB and the main container to 94.1 GB;
  - the recovery partition moved to the new end, and 55 GB was free;
  - growing again to 125 GB made a 119.1 GB container with 79 GB free.

  Raw disks need their recovery partition freed on the host (decision 38's flagged exception); ASIF needs nothing beyond Tart's documented command. On the host, the resize cost the moved partition's 5.4 GB, and nothing more until the guest writes.
- **Tools.** Software Update in the guest offered "Command Line Tools for Xcode 27.0-27.0", the generation the facts table pins for Darwin 27. MacPorts 2.12.6's Golden Gate installer is published at the address setup uses: 200, 7.4 MB.

## Changed

- **Setup takes ASIF with Tart 2.39.0 or newer** (`tart.HandlesASIF`, `ASIFReady`). On an older Tart, a source or a copy with an ASIF disk is still refused while it is a stopped clone, before it ever runs, and a copy falls back to a new image as before. A disk in any other format is refused by name.
- **`Configure` sizes by format.** A raw disk gets 100 GB, and its recovery partition is freed as before. An ASIF disk gets 125 GB and nothing on the host is edited. At 100 GB a Golden Gate guest has 55 GB free, short of the 60 an Xcode image stages its archive in; at 125 GB it has 79 GB free.
- **A raced listing is listed again** (`ErrListingRaced`); see below.
- **The facts table** has Darwin 27's tools row, and the test no longer exempts releases newer than Tahoe.
- **The live test** that expected the listing to fail now expects it to answer: `TestLiveTartListsWhileAnASIFVMRuns`, which skips on a Tart older than 2.39.0.
- **The docs** name the Tart it needs: tart-provider.md, usage.md, build-platforms.md, development.md.

## Made and checked

- **`dockhand providers setup tart golden-gate` took 2 minutes 40 seconds.** It made `dockhand-base-golden-gate` with the Command Line Tools 27.0 (27.0.0.0.1788430756, the build the table records) and MacPorts 2.12.6. The image is 125 GB, 41 GB on the host, and its golden copy shares its blocks. Setup's readiness probe of the guest agent over `tart exec` answered, with no sign of the control-socket wedge (#1346).
- **A check of `tree` on it passed** in 4 minutes 22 seconds, in the scratch clone and database.
  - Most of the host's time was the first port index for Darwin 27, now cached.
  - The guest reported macOS 27.0 26A428 arm64 with the tools above, which is what the pull request's *Tested on* line reads.
  - The clone was deleted.
- **The facts table**, regenerated from a fresh probe of all eleven images, gained one row: Darwin 27, arm64, the tools profile, clang 2100.3.34.2, SDK 27 (with 26.5 beside it). The other ten Tart rows came out identical but for the probe's date, which cross-checks that nothing had drifted.

## Found: a listing can race a delete

The first probe of the eleven images failed four of them with Tart's own "The file “config.json” couldn’t be opened because there is no such file", and left one probe clone behind, since removed.

Tart's `list` checks each VM's directory is whole, then reads its `config.json` again to size it. A VM another process deletes in between fails the whole listing. The code is the same in 2.37.0, so the race is old; two probes running at once met it.

Dockhand lists while something else deletes in several places: a check waiting for a VM slot while another check's clone goes, `clean`, setup, the harvester. So `tart.Client.Images` now names the failure and lists again, up to five times, half a second apart. The second full probe went through without a failure. The bug was reproduced without a VM booted, with 122 of 398 listings failing while another loop deleted VMs. It was filed upstream, with the person's approval, as [openai/tart#1353](https://github.com/openai/tart/issues/1353).

## Tests

- The version check, over Tart's printed versions.
- 2.39.0's listing and description of a running ASIF VM, with the null capacity.
- A raced listing answers on a later try, or says why after the last.
- Setup, with an ASIF source and with an ASIF copy, on an old and a current Tart.
- `Configure` gives an ASIF disk 125 GB and touches no host file, and refuses an unknown format before setting anything.
- The live test on the real Golden Gate image passed in 51 seconds.

## Left

- **The Xcode add-on for Golden Gate** needs an Xcode 27 archive. `providers setup tart golden-gate --xcode <.xip>` should then work, with 79 GB free for its 60, but it hasn't been run.
- **#1345** is fixed upstream but unreleased, and **#1346** is still open. The guards for both stay: a delete is trusted only by the VM's absence, and checks reach guests over SSH. Setup's agent readiness probe is the one `tart exec` left.
