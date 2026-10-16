# Wording given its home, config.Defaults, and the engine's file moves

Part 1's hygiene, third step: the command package review's 2a and 2b
items, `config.Defaults()`, and what's left of M1 (the architecture plan's
engine file moves) before layer 1.

- **Wording with a home** (2b). `thousands` is `prose.Count`. engine/words.go
  gains `PatchesApply`, which update's report and `CoverageWords` both say
  (it was composed twice); `ReleaseSource` and `ReleaseWords`, the forge
  vocabulary command's `releaseLabel` and `releaseWords` held; and `Ago`,
  beside `AgeWords`. `describeScope` already used `prose.And`.
- **One editor launch**: `editorCommand` and `runEditor` (verbs.go), which
  edit and submit's description both use (X5's "one editor launch" too).
- **One terminal predicate**: `errTerminal`, `terminal`, and the new
  `outTerminal` are `onTerminal` of a stream, with one `--json` rule. This
  fixed a bug: watch's `outputTerminal` read `streams.Out`, which Run wraps
  in a `switchWriter`, never an `*os.File`, so `dockhand watch` on a
  terminal always printed plain lines and never drew its live view outside
  tests. `onTerminal` looks through the wrapper; run under `script`, a
  build of this commit draws the view, and a689ae57's prints lines.
- **`github.PullRequestURL`** for `open`, which wrote the address by hand.
- **`TidyGroup.SetSubject`** (2a): a subject a person gives at tidy's prompt
  settles the block a missing one made, which the group records as it
  adds it, where `editSubjects` dropped every blocking line containing
  "needs a subject".
- **`config.Defaults()`**, one table of every setting's default as
  `dockhand config` shows it, built from the constants the accessors fall
  back on (`DefaultForOutdated`, `DefaultOutdatedAt`, `DefaultSubmitLimit`,
  `DefaultCapacity`, `DefaultGitHubCapacity`, with `DefaultCleanupAfter`
  and `DefaultMinFree`), so config's view retypes none. The two timeout
  defaults move to buildenv, `DefaultTestTimeout` and
  `DefaultBuildTimeout`, which the Tart provider falls back on in place of
  its own 30 minutes and `tart.BuildTimeout`; a test holds the latter to
  `ghactions.JobCap` (D16). Tests hold the table to the accessors and every
  row config shows to the table. Cleanup's `After` comment said 7 days
  where the default is 15.
- **Engine file moves** (M1's three left): restore and the checkpoint reads
  from tidy.go to restore.go; periodic cleanup (`CleanupReport`, `Cleanup`,
  `CleanupDue`, `StampCleanup`, and their constants) from clean.go to
  cleanup.go; and environment wording (`DescribeEnvironment`,
  `EnvironmentHeading`, `releaseWords`, `describePlace` from runner.go,
  `providerWords` from body.go) to words.go. No code changed in the moves.
