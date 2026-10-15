# Dead code and the tests that kept it

The person asked for a pass over tests and code that no longer match how
dockhand works. `deadcode -test`, which CI runs, counts tests as callers, so
it can't see code only a test reaches. Three passes found it: `deadcode`
without `-test`, a typed count of each declaration's uses split by
production and test, and a read of every package's tests against the code
they exercise and the activity notes that changed it.

Deleted, with no change to what a command does:

- **Code only tests reached:** `Snapshot.RequiresXcode` (production reads
  `PortInfo.Bool("use_xcode")`), depblock's `Tools.Probe` and `Availability`,
  `syntax.Error.Describe`, the evaluator's `Prefix`, editprep's
  `MaxDownloadBytes` (nothing set it), portedit's `GoToolchain.Unmet`,
  `Workspace.Source`, `Batch`, `Edits`, and `Commit` (portedit opens its own
  session and editprep writes the tree), the form `StealthDistSubdir` returned
  and its one caller dropped, the mirror's `Margin` (a constant now),
  `project.ParseCMake` (its tests read through `Reading.CMakeDocument`),
  engine's `ServeCandidates` wrapper, `github.Client.Authenticate`,
  `registry.Client.Source`, `textedit.Span.Bytes`, `sqlite.Store.Path`, and
  command's `testTart` seam, which no test set.
- **Fields written and never read:** `Created.Project`, `ReviewReport.State`,
  `TidyPlan.Worktree` (always `Branch.Worktree`), `surveyedPorts.e`, and
  `forge.PullRequest.Status`; `targetWords`' unused target parameter.
- **v2's forge contracts:** `forge.Accounts`, `forge.PullRequests`, and
  `forge.PullRequestInspector`, which `engine.Forge` replaced, with
  `github.Client.Name`, which only `Accounts` asked for.
- **Unused aliases:** engine's `Observation` and `StealthDistfile`.

Moved or corrected:

- `ownedSections` is a test helper, so it's in body_test.go.
- `TestSetupWritesTheMaintainerLine` called `config.SetMaintainer` alone; it's
  `TestSetMaintainerWritesTheLineBesideWhatsThere` in internal/config.
- `undo --json` was refused while its hidden alias `restore --json` worked;
  `undo` is on the `--json` list, and `TestTidyAppliesDockhandsOwnEditsAndUndoPutsThemBack`
  reads its envelope. `open`'s JSON result, which no `--json` run could reach,
  is gone.
- `dockhand config`'s maintainer default says create writes `nomaintainer`,
  as it does, not that create asks.
- docs/development.md's table of opt-in tests names the tests and packages
  as they are, and drops `DOCKHAND_TEST_PORTS_REPO`, which nothing reads.

Kept, as the roadmap still plans them or the call is the person's, is in
the pull request's description.

Tests: the whole suite, vet, lint (Linux and darwin), and `deadcode -test`.
