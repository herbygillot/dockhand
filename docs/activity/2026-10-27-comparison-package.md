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
- Not yet: compose builds the collector and the preparer, with
  `discovery()`, where the evaluator exists (the shape's point 4).
