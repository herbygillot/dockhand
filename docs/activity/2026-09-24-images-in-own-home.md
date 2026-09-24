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

## Proofs

- **Tahoe, provisioned fresh.** `dockhand setup --os tahoe --rebuild`
  built `dockhand-base-tahoe` in the new home (MacPorts 2.12.6, guest
  agent 0.14.1, Command Line Tools 26.6), bootstrapping with the image's
  password once and installing dockhand's key; `setup --check --os tahoe`
  then reached it with the key alone.
- **Verification over SSH.** The real acceptance test,
  `TestRealTartBuildSurvivesSubmittingDriverExit`, passed against it in
  73 s: staging, launch, status, result, and logs all over the channel.
