# The split's small relocations

Part 1's hygiene, fourth step: the command package split's rows that land
in hygiene (plan/command-package-split-coalesced-2026-10-15.md; the
command package review's §7a, §7b row 1, and §7d rows 3 to 5), so `term`
can be cut as a file move.

- **7a rows 4 and 15, Tart readiness:** `tart.Status.Readiness()`, typed as
  `NoImage`, `XcodeOnly`, `NotForHost`, and `Ready`, which setup's Providers
  row and its `Next:` steps word; Tart not installed, or a Mac that can't
  run it, stays command's, being before there's a Status. `Setup` fills
  `SetupResult.HostMacPorts`, this Mac's MacPorts where it isn't the
  image's, which command printed after computing it itself.
- **Row 5, `macports.FindTclsh`:** port-tclsh on PATH, else under the
  default prefix. The evaluator uses it where no executable is named, so
  it too finds /opt/local/bin's when PATH lacks it, as setup's report did
  and it didn't. command stops naming the tclsh: `engine.Options.Tclsh`
  defaults to it, and setup's MacPorts row reads `Engine.Tclsh()`, so
  command keeps its import boundary.
- **Row 6:** `config.Tilde` and `config.Absolute` replace command's `tilde`,
  `tildeOrEmpty`, `absolute`, and `homeDir`; config's `expandHome` shares
  the ~ expansion.
- **Rows 8, 9, 13:** `prose.FirstSentence`, `FirstLine`, `Wrap`, and
  `LastLines` (command keeps the indent); `firstOf` is `cmp.Or`;
  `portfile.Checksum.Words()`.
- **Row 12:** `yesTo` and `confirm` are `Streams.askYes`, decide's asking
  half. setup's offers now return a read error rather than swallowing it.
- **Row 14, C4b's typed half:** `model.EventKind`, with a constant for each
  kind the journal holds and `model.CheckpointEvent` for a checkpoint's;
  `Event.Kind`, the driver's `emit`, and `CountEvents` take it, and the
  engine's `GoneKind`, `AlteredKind`, and `ServeSubmitKind` are model's.
  check's journal matches the constants.
- **Row 16:** `github.StartSpend()`, a command's count of its requests, and
  `Spend.Log`, the `DOCKHAND_GITHUB_LOG` line; `github.Requests` goes.
- **7b row 1:** `engine.CleanupIfDue`, the due check, the stamp first, and
  the pass, with `CleanupReport.Words`, which serve's cleaner and
  `clean --automatic` both ran by hand.
- **7d row 3:** one `writeTidyPlan(out, plan, saved)`. Row 4: the
  `environmentWords` alias deleted, and the engine's own
  `describeEnvironment` with it. Row 5: `--json` is marked in one place
  since `auth` went, root's allowlist; `asksForJSON` stays, since an
  envelope is due when cobra refuses the command line before the flag is
  parsed.
