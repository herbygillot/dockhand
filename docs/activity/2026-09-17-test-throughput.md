# Test and evaluation throughput

Asked for on 2026-09-17: the suite had been getting slower; scan for where the time goes and what would make it cheaper, and pull the first two levers.

## Where the time went

The full suite, uncached, on an 18-core host: 232s of wall time, package times summing to 983s, so packages already overlapped about four to one. Not one test in the tree called `t.Parallel`, so nothing overlapped inside a package, and the four packages that dominate did so alone:

| Package | Time |
|---|---:|
| macports/portedit | 227s |
| workflow | 158s |
| workflow/preparation | 154s |
| cli | 136s |

The cost unit is the MacPorts interpreter. A bare `port-tclsh` with `mportinit` starts in 0.08s, but each of dockhand's evaluations loads its scripts and evaluates the Portfile in a fresh interpreter, and one archive preparation with modeled contexts starts 34 of them, about 9s alone and 15s under contention. `TestNativePlatformOperands` ran five of those to test operand handling, 170 interpreters, and took 78s. A git-fetched preparation, which has no modeled contexts, needs 6 interpreters and 1.2s. The 322s of system time in the run was that process spawning.

## Lever 1: parallel tests

548 top-level tests are now marked `t.Parallel`, plus the subtests of the two heaviest table tests. Nineteen tests stay serial, and every file that sets environment or changes directory stays serial as a whole, because a helper that calls `t.Setenv` cannot be reached from a parallel test; the first pass marked per test and two such helpers found that out. The workflow fixture's Git isolation (`GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM`) moved from the per-test helper into `TestMain`, where a process-wide setting belongs and where it no longer blocks parallelism. The race detector was run over every package that gained parallel tests and found one real race, in dockhand rather than in a test: the CLI set `cobra.EnableCommandSorting`, a package global, on every root construction, and concurrent roots raced on it. It is set once at package init now.

## Lever 2: concurrent observations

Every multi-context path in the editor observed its profiles one at a time: `planObservedArchives` (baseline and candidate per profile), `planObservedChecksums`, `assessArchives`, and the final per-context evaluation in `applyObservedArchives`. Each observation wrote the contents to the workspace Portfile, started an interpreter, and restored the file, so observations of one contents could not overlap. `observeProfiles` now writes the contents once, observes every profile concurrently on the read-only file, each in its own interpreter, bounded to eight at a time, and restores once; results keep the profiles' order and the baseline cache is consulted and filled as before. This speeds real bumps and assessments, not only tests: the modeled contexts of a bump are the same observations.

## Result

Full suite, uncached, same host: 232s to 101s of wall time; package times sum to 600s from 983s. The interpreter count per preparation is unchanged; that is the next lever, recorded on the roadmap: reuse one native session across resolve, baseline, candidate, and final evaluations, which the batched version probe already does, and test operand and regeneration behavior at their unit seams with one or two end-to-end cases each.

One flake surfaced once under the full-suite load and did not reproduce in fifteen runs under equivalent load: the GitHub provider's log-retention test saw a prune of a wrong run return without error. Its assertion now reports what prune returned, so a recurrence explains itself.

## Not taken

Of the libraries considered, `golang.org/x/sync/errgroup` is the one added, for the concurrent observations. `testing/synctest` is in the standard library and available for the engine's time-driven tests when one needs it; `gotestsum` is worth using as a tool for per-test timing; `testscript` would make the cli package's end-to-end tests consistent but does not remove the interpreters, so it is a migration for another day.
