# 2026-09-24: images in dockhand's own home

Step 2 of the roadmap's Next: the verification images move into
`~/.dockhand/tart` and are reached over the channel.

- **SSH directory override.** `channel.DefaultKeys` honors
  `DOCKHAND_SSH_DIR`, as `tart.Client.Resolve` honors
  `DOCKHAND_TART_HOME`. Host keys are recorded by image name, so a second
  Tart home needs its own SSH directory; `docs/usage.md` says so. Test
  binaries with opt-in live Tart tests pin it to the real
  `~/.dockhand/ssh` (`testsupport.IsolateHomeKeepingTart`), since they
  isolate `HOME` and would otherwise look for keys that are not there.

- **A quiet tools probe.** `macos.CheckCompiler` asks `xcode-select -p`
  with its output discarded: on a fresh guest it is expected to fail, and
  setup streamed its "xcode-select: error: Unable to get active developer
  directory" into a provision that went on to succeed.

## The migration

`dockhand setup --os <release>` for Monterey, Ventura, Sonoma, and
Sequoia, and the same with `--xcode ~/Downloads/xcode_archives`, found
each image missing from dockhand's home and present in `~/.tart`, and
copied it: `tart export` from the person's home, `tart import` into
dockhand's as the candidate, a bootstrap connection with the password
that installed dockhand's key and recorded the host keys, validation
against the profile, and adoption with a golden copy. Each took about a
minute; the images in `~/.tart` were only read. The Tahoe Xcode image was
made fresh (`--rebuild`), on Command Line Tools 26.6 and Xcode 26.6, the
2.2 GiB archive crossing the channel checked in six seconds; the one in `~/.tart`
has the macOS 27 tools. `setup --check` then passed for all ten images
with dockhand's key alone.

| Release | Tools | Xcode image |
| --- | --- | --- |
| Monterey | 14.2 | Xcode 14.2 |
| Ventura | 14.3 | Xcode 15.2 |
| Sonoma | 16.2 | Xcode 16.2 |
| Sequoia | 16.4 | Xcode 26.3 |
| Tahoe | 26.6 | Xcode 26.6 |

## Proofs

- **Tahoe, provisioned fresh.** `dockhand setup --os tahoe --rebuild`
  built `dockhand-base-tahoe` in the new home (MacPorts 2.12.6, guest
  agent 0.14.1, Command Line Tools 26.6), bootstrapping with the image's
  password once and installing dockhand's key; `setup --check --os tahoe`
  then reached it with the key alone.
- **Verification over SSH.** The real acceptance test,
  `TestRealTartBuildSurvivesSubmittingDriverExit`, passed against it in
  73 s: staging, launch, status, result, and logs all over the channel.
- **Every release, over SSH.** `verify tree --working-tree --os
  available --fresh`, at the default capacity of two, passed on Monterey,
  Ventura, Sonoma, Sequoia, and Tahoe in ten minutes, the first hashes of
  the Ventura and Sequoia images included.
- **Growing logs arrive whole.** `verify jq --working-tree --os monterey
  --os sonoma --fresh --trace`, at capacity one so the three builds'
  traces do not interleave, passed on all three, and each saved build log
  (about 245 KB) appears byte for byte and contiguous in the traced
  stream, which read it in 64 KiB ranges while it grew: the loss
  `tart exec` showed on Monterey and Sonoma (#1347) is gone. Unrelated to
  the channel: jq's advisory tests fail on Monterey and Sonoma, where
  `make check` rebuilds `src/main.o` and is refused "Operation not
  permitted"; on Tahoe it does not rebuild. Not pursued.
- **The two-VM limit is the Mac's, and Tart names only its own home's
  VMs.** With two throwaway clones running in dockhand's home, a third VM
  in a scratch Tart home was refused "The number of VMs exceeds the
  system limit", naming no VMs; the same start in dockhand's home says
  "(other running VMs: limit-b, limit-a)". So the person's VMs count
  whatever home they run from, and Ready matches only the message's
  first part. The throwaway VMs and the scratch home were removed.
- **ASIF.** An image copied from the person's home is declined before it
  runs when its disk is ASIF, as a source is, and provisioned afresh
  (`TestAnImportedASIFImageIsDeclinedBeforeItRuns`, beside
  `TestASIFSourceIsDeclinedBeforeItRuns`). No ASIF image was at hand to
  prove it live.

The images an earlier dockhand made in `~/.tart` are still there, untouched;
they are the person's to delete.
