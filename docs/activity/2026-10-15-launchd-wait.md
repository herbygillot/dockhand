# serve --install and --uninstall wait for launchd; the agent moves to macos (v0.4.0 part 1, c)

The rc10 full stage's B7, and rc6's: `launchctl bootout` returns before
launchd has unloaded the agent, so a status run at once read serve's agent
still there, and `--install`'s bootstrap after a bootout could meet the job
still loaded. With it, the command package split's 7a rows 1 and 7, as its
coalescing placed them.

- **`macos.LaunchAgent`** installs and removes a per-user launch agent,
  waiting on launchd: `Remove` boots it out and asks `launchctl print`
  until launchd no longer has it, up to thirty seconds, removing its
  property list either way and returning `ErrStillLoaded` where the wait
  ran out; `Install` waits for the agent it replaces to go before
  bootstrapping, and for the new one to load. `Loaded` reads "Could not
  find service" as not loaded. Its one seam is the launchctl runner.
- **serve's agent uses it** (`serveAgent`, `agentJob`), so serve's own
  launchctl calls, plist writing, and bootout-then-bootstrap go.
- **`macos.Notify` and `macos.Open`** are the notification and the
  browser, which command kept as private helpers (`postNotification`,
  `openBrowser`, which tests still stand in for).
- **tart/host's launchd.go goes**: v2's `Machine.Start` and `Stop`, which
  nothing calls, kept by PR #4 only as the wait's pattern, which
  `LaunchAgent` now is.
- **B7's harness wait goes with it**: the row reads launchctl once, at
  once, after `--uninstall`, and fails where launchd still holds the
  agent.

Tests: TestALaunchAgentWaitsForLaunchd; TestServeInstallsALaunchdAgent and
TestServeInstallCarriesTheSettingsItRanWith, with launchctl stood in by
`withLaunchd`, which lets an agent go some prints after its bootout.
