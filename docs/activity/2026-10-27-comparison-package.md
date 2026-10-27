# The upstream comparison moves to internal/comparison

2026-10-27, Then's item 15 (C8-7), its second part, in the shape the
architecture thread gave (the coordinator's relay, 14:42Z).

- **`internal/comparison`**, type `Collector`, holds what collecting a
  port's upstream comparison is. It moved from the engine, about 600
  lines:
  - assessPort, as `Revision`;
  - assessUpstream's comparison half, as `Update`;
  - collect, patchesFor, readCommits, readPlans, readPairs,
    readDistfile, observeProviders's loop, the mirror, commitKey,
    binaryReason, and the coverage policies, as `PolicyNotCompared` and
    `PolicySourceUnchanged`.

  Both methods return a `model.UpstreamComparison` with its Problem and
  ProblemKind set inside.
- **It reads through what it's given**: ArchivePlanner, SourceArchiver
  (or why there's none), a Git `Resolver`, a tree's `Files` for patches,
  an `Observer` for the ports a requirement names, the mirror, and the
  readings cache. The candidate's Portfile comes in as bytes from the
  engine, so neither git nor the store is imported, which its boundary
  test holds.
- **The engine keeps orchestration and state**:
  - revisionAssessments and makeAssessments' choice of what to assess;
  - the trust gate and lookAgain;
  - recording;
  - assessUpstream's nil-where-nothing-said;
  - the readers of a result (SourceUnchanged, NothingCompared,
    ComparisonLines, CoverageWords).

  Its `collector(planner)` builds one over its adapters: `gitResolver`,
  `treeFiles`, and `portObserver` (with `providerPort`), or
  `unreadablePorts`.
- **Tests**:
  - The planner fake moved to `comparison/comparisontest`, as `Planner`
    and `WriteTarball`, which the engine's tests alias.
  - The tests of the readers moved into the package: the base's dropped
    patches, a removed archive, a distfile not extracted, and a binary
    distfile's words.
  - The engine's tests go through the collector.
  - TestARevisionIsAssessedByItsNetChange now also asserts that the
    recorded assessment is the collector's comparison.
- **compose composes them** (the shape's point 4, architecture's (b) on
  2026-10-27: not engine-held defaults, which would keep construction
  with two owners). compose builds, in order:
  - the evaluator's selection reader (`Engine.SelectionReader`);
  - upstream discovery over it, `upstream.Service` with GitHub's catalog
    (`Engine.GitHubClient`) and GitLab's;
  - the port editor, `editprep.Service`, and the forge archiver, which
    is discovery.

  It sets `e.Discovery`, `e.Preparer`, and `e.SourceArchiver`. Where the
  evaluator can't be had, it leaves them unset.
- The engine's lazy assembly of the three is gone: `preparer()`,
  `discovery()`, and `sourceArchiver()` now return what compose gave, or
  say none is set up, at the point of use. outdated reads
  `e.Discovery`. A test that needs one gives it, as
  TestWhatCantBeComparedSaysSo gives a stub archiver.
- Kept: the per-call `collector(planner)` builds the Collector from what
  the engine holds, since a planner is the engine's capability (Ports)
  and the Git resolver and tree files are its adapters. The selection
  reader's own lazy assembly stays in the engine, which every reading
  uses.
