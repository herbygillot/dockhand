# 2026-09-23: the architecture review, checked against the code

The [review](../reviews/2026-09-23-architecture-review.md) is three
read-only passes over the whole tree at `9b00714` with a reachability
pass beside them. Every claim below was checked against the tree at
`6182c77d`, which is `9b00714` plus the evening's dead-code removal and
the reconciliation; the review's list of unreachable functions was
already five shorter by the time it was read, and it says so itself in
naming them.

## Verified

- **The three defects.** `use_xcode` is read three ways: the snapshot's
  reader knows case, `on`, and `off`; the scope rebinding and the fidelity
  scope accept `yes`, `true`, and `1` only, so `use_xcode on` records
  `NeedsXcode` false. `extract.rename` is read as yes-words at two sites
  and as not-no-words at a third. GitHub verification locks a fork branch
  under `verify.ProviderGitHub` and publication under the forge's name,
  both the string `github` by separate definition. The job-log download
  uses `http.DefaultClient`, not the injected client, with no bound, and
  is copied to disk unbounded.
- **The copies.** The preparing-action set is in `record`, the phase
  table, the verification plan, and the scheduling SQL, and the three
  update actions in three more places, the resolution's `updates` among
  them. Cancel validation is in `workflow/control.go` twice and in
  SQLite. `app.preparationSource` is `fetchMaster` word for word, and
  adoption calls it. The provider policy is decided in `cli/build.go`,
  `app/verification.go`, `cli/actions.go`, and `workflow/choice`. The
  checksum vocabulary is `portfile.checksumKind` and
  `distfiles.algorithm`. The guard grammar's `switchReason` and
  `loopReason`, written the same evening, re-parse switch arms and loop
  words that `syntax.Command.Control` already separates.
- **The doubles.** Both providers keep an execution ledger with a lock,
  a read, a put, and unknown-request closing; both `provision` and
  `verify/tart` inspect an image, writing the manifest path as a literal;
  `provision`'s native has its own clone, stop, images, and start; the
  revision is found by syntax in `portfile` and by declaration frames in
  `portedit`; `portindex.Stage` then `Open` is written five times; the
  fork's owner is checked in `publish.Destination` and again in
  `app/github_verification.go`.
- **The concentrations.** Fifteen `State`-and-`Repository` guards, 51
  state calls carrying the repository, twelve reaches into
  `Publisher.Forge`, seven engine fields set only by tests. `publish`
  imports `portedit` for `IsAttribution` alone. The guard grammar is 927
  of `eval`'s lines with one call site. The `cli` submission tail is
  written three times with "accepting request" and once without.
- **What can go.** `dockhand review accept x` against an absent database
  creates it before saying the command is not wired. `state.Query`'s
  `Newest`, `WithBuild`, and `Action` are implemented in SQL and set by
  no command. `PreparationRequest.Selection` survived the resolution's
  landing unread. `Services.Preparation` is never read. `Prerequisites`,
  `Inputs`, and `ArtifactRequirement` have no reference outside
  `record`. `Reader.Revisions` and `SubmissionsForAttempt` have no
  production caller. `cli.NewRoot` is reached only by tests. `PlanSingle`
  has no caller. The `Projected()` branch in `app/selection.go` never
  runs in production. Two aliases, five `digest` helpers, nine
  `/var/tmp/dockhand2` literals beside a constant, `scratch` on
  `syscall.Flock` where `filelock` exists, the doubled `NewTreeOver`
  paragraph, seven exported names with no user outside their package,
  `tcl/shell` with two importers. The unreachable archive fallback in
  `applyArchivePlan` is plausible, `plan.observed` being nil only on a
  path the observer now always takes, and the review's own gate, a test
  proving it unreachable before deletion, is the right one.
- **Documentation.** `components.md` does not mention `scratch`.

## Corrected, in the review

- `changeset` knows the tree's layout, `_resources` and the two-level
  directory, and no port; the component map's sentence, that it does not
  interpret changed paths as targets, is what the code does.
- No production read of `Engine.Provider` remains; the conclusion is
  stronger than stated.
- `macports/selection` is 186 lines, not 75.

## Accepted, and in what order

The review's order is taken as the next queue's shape: the three
defects; the deletions, with the archive fallback gated on its test; the
commit-message and fetch-guard leaves, which cut `publish`'s closure
from 28 packages; the single rules, `PortInfo.Bool`, `JobPhase.Next`,
`ValidCancel`, one action set, one master fetch, one checksum
vocabulary, and `Control` in the guard grammar; the contracts one at a
time, `state.Scoped`, the forge interfaces, the ledger, the index
source; and the request values with the command pipeline once the
surface settles. Not recommended and not taken, as the review says:
splitting `state.Reader`, one observation interface over both
providers, an interface over `portedit`.
