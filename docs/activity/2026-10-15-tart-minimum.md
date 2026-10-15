# check and serve refuse a Tart older than 2.39.0 (v0.4.0 part 1, c)

The rc9 full stage's F8: with a Tart reporting 2.38.0, check-100 started a VM
and passed. The minimum, 2.39.0, for openai/tart#1344's listing blocked by a
running ASIF VM, was held in setup and provisioning alone. It's one of
v0.3.0's known issues.

- The Tart provider's `Environments`, which check and serve's checks plan
  through before any VM starts, reads Tart's version (`machine.Version`) and
  refuses one older than `tart.MinimumVersion()`: "Tart 2.38.0 is older than
  2.39.0, which dockhand needs: it can't list its VMs while one with an ASIF
  disk runs (openai/tart#1344); sudo port upgrade tart".
- TestEnvironmentsAreReleasesWithImages covers it; the provider's fake Mac
  reports a version.
