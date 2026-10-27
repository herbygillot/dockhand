# port lint's warnings are said; a new port's count as ✗

2026-10-27, Then's item 11, the docs dig's I13 (the person's "I think we
can take I13, I24 & I26.", 2026-10-26 16:26Z).

- **The plan** marks a port the branch adds (`PlanTarget.Added`), whose
  Portfile the base hasn't.
- **The guest** lints such a target with `--nitpick` and keeps every
  target's `Warning:` lines from its lint step (`Result.Lint`,
  `Nitpick`). Lint fails on errors alone, so nothing is built or judged
  otherwise: the protocol stays 3 and the guest pin moves.
- **The github provider** reads them from the lint step's annotation,
  "port lint NAME:%0AWarning: …", which MacPorts' workflow writes as a
  warning, or as an error where lint failed (cilog `Built.LintWarnings`).
  The workflow lints without `--nitpick`.
- **The result** keeps them, with whether lint nitpicked (schema 48,
  `results.lint` and `results.nitpicked`).
- **Said** under "Lint" in the check's report and status's detail
  (`evidence.LintFindings`, `view.LintLines`). An update's warnings are
  notices (!). A new port's are ✗. A new port linted without `--nitpick`,
  as on GitHub, says its nitpicks weren't read.
- **Holds**: a new port's warnings are a publication block
  (`branchstate.LintBlocks`, `BlockLint`). Submit refuses until they're
  fixed or taken with `--accept <port>` for that commit, and a passed
  check is not publishable until then. Status's row is `lint-warned`, with
  `submit --accept` for its Next.
- Tests: TestLintsWarningsAreKeptAndANewPortIsNitpicked,
  TestLintsWarningsAreReadFromItsAnnotation,
  TestANewPortsLintWarningsHoldUntilAccepted, TestWriteLint, and the
  store's result test.
