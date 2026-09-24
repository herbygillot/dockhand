# 2026-09-24: no newer-OS gate

Step 1 of the roadmap's Next; decisions 4 and 5 of the [contracts
direction](../reviews/2026-09-23-contracts-direction.md). Tart builds on
the host's release by default, and dockhand does not refuse a release for
being newer: Tart is the one to refuse what it cannot run, and the Golden
Gate test showed it runs a guest newer than its host.

Two refusals went:

- `setup` on a Mac newer than `tart.DefaultDarwin` asked for `--os`
  (`app/setup.go`). It now prepares the host's release, as it does on
  any other Mac.
- An unnamed Tart build on such a release was refused with a request for
  `--image` (`verify/tart/config.go`). It now builds on the host's image.

With them went `tart.DefaultDarwin`, `DefaultRelease`, and
`NewerThanDefault`, which nothing else read. `verify.BuildOptions.Named`
stays: a missing image for a named release asks for `setup --os
<release>`. `macos.CurrentDarwin` stays too, as the release a host that is
not a Mac models; its comment no longer says a Tart build uses it.

A release the table has no name for is still refused where a release
must be known (image names, sources, the installer); that is a missing
fact, answered with `--source` or `--image`, not a gate.
`TestBuildConfigDoesNotGateANewerRelease` replaces the test of the
refusal; `docs/build-platforms.md` and `docs/cli-design.md` say so.
