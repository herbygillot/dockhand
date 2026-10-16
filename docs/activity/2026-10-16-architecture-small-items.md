# The architecture review's small items

Part 1's hygiene, fifth step: X2 finished, C4c, X4, X5, H1, and H2 from the
architecture review of 2026-10-05 (the review artifact; plan/
architecture-resynthesis.md), with C4d and two sub-items declined, each
said below.

- **X2, finished: only infrastructure is tried again.** The runner retried
  every error that wasn't `ErrNeedsAttention` or `ErrDockhandFault`, so
  `ErrInfrastructure`, wrapped 35 times by the providers, had no reader. It
  is read now: an error wrapped as infrastructure is tried again, up to
  `model.MaxAttempts`, and any other is said at once as dockhand's,
  "dockhand: <environment>: <error>; the provider didn't mark it as
  trouble another attempt could clear, so none was made", as the review
  proposed. The providers' unmarked returns were read for what this
  changes: tart's are a platform this dockhand doesn't know, a local
  directory or staging failure, and the store's own writes; GitHub's are
  the run's waits ending with the context, its records, and local files,
  every network read being wrapped already; none is one a VM's next attempt
  clears. The test provider's flaky VM is wrapped as infrastructure, as
  the real ones are, and the "unclassified" case now stops at one attempt.
- **C4c:** `UpdateRequest` embeds `model.EditIntent`, passed whole to
  editprep, where it copied `SharedRelease` and `KeepOldChecksums`.
- **X4:** `model/boundary_test.go` (model imports nothing of dockhand's);
  `macports/boundary_test.go`, naming each of the top package's five
  imports and why, and walking every macports package for one that reaches
  store, coord, engine, command, or buildenv; command's boundary test
  gains the engine's unused-entry loop, which took `internal/subprocess`
  off its list; config's two imports are said where they're made.
- **X5:** the store no longer invents `Branch.EndedAt`: a branch leaving
  open must say when, or the store refuses it, and the engine says: the
  pull request's observation time where it merged or closed, the clock's
  where it was archived. architecture.md: a paragraph on who takes the
  branch lock (the three history transitions) and what the other verbs
  that move a ref rely on instead (capture's double read and the ref's
  compare-and-swap); its four wrong sentences fixed (composition is still
  split, editprep reaches `scratch` through `macports/workspace`, history
  records and settles while the verb makes the Git change, and the
  variables, now a paragraph naming all the person may set); the reading
  cache's row added. The editor launch was folded in 947b5382.
- **H1:** `model.ObjectID.Short()`, deleting engine's `short` and `Short`,
  history's, commitrules', and prdescription's slicing by hand.
- **H2:** `commitmsg.SubjectPorts`, MacPorts' "a, b: what changed" rule; the
  generic GitHub adapter's `OpenPullRequests` returns its search's pull
  requests, and the engine keeps those whose title names the port.
- **Declined, with why:**
  - *C4d*, `portedit.Assessment` to tools/survey: `VersionProbe.Assess`
    reads nine of portedit's unexported internals (the probe's input,
    carriers, `prepare`, `assessArchives`, `dependencyBase`,
    `inspectDependencies`), which a move would have to export, the cost the
    review weighed against the engine split. It stays, survey-only.
  - *X4's `byHand` taking the typed cause*: the two sentinels it trims are
    wrapped `fmt.Errorf("%w: …")` at 86 sites, so the reason after them
    exists only as text; typing it is a change to every site.
    `archive.ErrBinary`, the other trim the review named, is typed already
    (`BinaryError`, read by `binaryReason`).
  - *X5's clocks for serve's cadence*: the `time.Now()` left in serve are
    intervals between refreshes and looks, not recorded times, and an
    injected clock that a test holds still would stop them.
