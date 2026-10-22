# Part 5: the interface on top

The roadmap's part 5, after part 4's verdict and split.

## R3-34: status's JSON gives the verdict

`status --json`'s branch had its latest and active checks, and left the
standing to be judged from them again, though `CheckStanding` exists on
the value the view reads. Each branch now has `check_standing`,
`branchstate.Standing`'s name (`Standing.String`): `not-checked`,
`checking`, `stopped`, `passed`, `incomplete`, `older`, `failed`, or
`attention`. Serve's state was already fields (`serve_state`) beside its
sentence.

## Item 1, §3's chaining

### tidy's message composer is commitmsg's

This is the code-organization review's finding 9. Its dead half
(`Compose`, `Rewrite`, portedit/message.go) was already gone.

- The live composer moved from engine/tidy.go into commitmsg, beside the
  rules it writes to: `Compose(subject, text, combined messages,
  generated)`, `Body`, and `SplitTrailers`.
- engine/body.go's PR description reads `SplitTrailers` from there too.
- create's subject is "<port>: new port", as Design v3 §8 and tidy's
  `derivedSubject` give it, where create wrote "…, version X".

### One Make for a history transition

Finding 26's remedy is `history.Transitions.Make(ctx, &checkpoint,
history.Change{Failpoint, Move, After, Made})`. The checkpoint is
prepared, the acceptance harness's kill step is hit, the change moves
(abandoning the checkpoint where it fails, after undoing what it did
itself, as rebase's kept ref is), and the checkpoint settles as applied
with After's event message. An error from After leaves the checkpoint
prepared, for the next lock to settle. tidy's apply and rebase each
wrote that sequence out; both make it now. `TestMakeKeepsAChangeInACheckpoint`
holds the three ends.

### submit folds in tidy's commits

PlanSubmit's two refusals, edits not committed and no commits above
master, are one typed `engine.Uncommitted`, worded as before. The submit
command commits them first through tidy's stage, then plans the
submission of those commits. ship's `tidyBranch` is now `tidy.Stage`,
which both take. Under `--plan` it shows the commits and makes none
("… commits first"), since the submission's preview is of their commit.
`--head` submits the committed head alone, as before. boundary_test's
`verbImports` names submit → tidy beside the composites, and submit's
merge check stays.

### tidyrun, the stage three verbs run

By architecture's answer, tidy's stage is a layer, `internal/command/tidyrun`,
beside checkrun: `Stage`, `Decide`, `Apply`, and `EditSubjects`. The tidy
verb is its front, and ship and submit import tidyrun, not tidy. The
`submit → tidy` entry comes out of verbImports, and ship's tidy entry
with it.

## Item 2, §10's marks

The marks are view's (view/marks.go), one meaning each across commands:
`✗` broken, `!` look at this, `✓` done, `⋯` under way. `·` is only a
separator, and a line for your information carries no mark, keeping the
mark's column (`view.Unmarked`).

- **Holds are said in a word.** An upstream finding that holds reads "!
  held: LICENSE changed", and one that doesn't is unmarked, where `!`
  meant it held and `·` that it didn't. The legend that explained them
  ("! holds bump's and serve's submission…; · holds nothing") is gone
  from update, submit, and the upstream report. A moved Git source reads
  "! held: …" in submit's Upstream lines, and archives not compared read
  "! held: upstream archives not compared: …".
- **Status's ready row** ("passed; waiting for you to submit", "opened
  by serve…") is `✓`, where it was `·`.
- **Result cells**: a target an environment can't build reads "! not
  built: needs Xcode", one not run "! not run", and another outcome "!
  interrupted", where each was `·`. The pull request's table takes them
  too.
- **setup**:
  - What you'd act on is `!`: "! needs Tart: …", "! not set up: dockhand
    setup tart", "! no image: …", "! Xcode image … ready; no plain
    image…", and "! needs a GitHub login…".
  - What's for your information is unmarked: "needs Apple silicon; checks
    here build on GitHub", the GitHub login not set up until you submit,
    and the maintainer line.
- **The rest that used `·` as a mark**: update's patch lines ("✓ N
  patches apply …"), its dependents' notes, the baseline's "not built at
  the base", review's lines ("! upstream's change wasn't assessed"), and
  tidy's note on older builds. These are now `✓`, `!`, or unmarked by what
  they say.
- Acceptance row B9 reads setup's "! not set up" line.

### Colour, and the width

- **Colour on the marks alone** (the person's "Marks only"): on a real
  terminal, standard output's `✗` is red, its `!` yellow where it's a
  mark (first on its line, or after a table cell's two spaces, before a
  space), and its `✓` green; nothing else is coloured (`term.markWriter`,
  which Begin wraps the output in). `NO_COLOR`, set and not empty, turns
  it off, and so do `--json`, a pipe, a test's stand-in terminal, and
  status --watch's `--plain` (`Streams.Plain`).
- **The width off a terminal** is `COLUMNS` where it's set, else 100,
  where it was 80. Nothing is cut, since field testing reads dockhand
  through a pipe. The root reads `NO_COLOR` and `COLUMNS` from its
  invocation's environment (`term.Display`), since nothing below compose
  reads the environment.
- **One prose writer**, `Streams.Hanging(first, text)`, wraps at the
  width with a hanging indent, as status's attention list does. Item 4's
  grammar takes it, with setup's rows. Cutting a table's widest cell with
  `…` on a terminal lands there too, with the tables it reworks.
- Three tests that read status's wrap at 80 read it at 100.

## Item 3: levels and the stage lines

### ship's line a stage

- **-v reaches the streams** (`term.Streams.SetVerbosity`, `Verbose`),
  which the root sets from its count before anything runs.
- **A stage's report is held back** at the default level
  (`term.Streams.Staged`): what the stage writes to standard output waits,
  and is said where the stage held or failed (`Stage.Say`) or asks a
  question on a terminal (`Ask` says it first, and what follows is
  written as it comes); a stage that went through says its one line
  (`Stage.Done`) and drops the report, which is `-v`'s. With `-v`, or
  `--json`, nothing is held. On a terminal, the stage under way is the
  line redrawn on standard error ("⋯ checking jq").
- **ship's stages are lines** under the change's heading:

  ```
  jq 1.7.1 → 1.8.1 (GitHub tag jq-1.8.1), on branch jq-1.8.1
    ✓ edited     textproc/jq/Portfile
    ✓ committed  jq: update to 1.8.1
    ✓ checked    check-1 passed
    ✓ submitted  opened #34901  https://github.com/macports/macports-ports/pull/34901
  ```

  A clean `ship jq --yes` was about 45 lines. A batch's checks are seen
  through after every change is queued, so each says its branch again
  over its check's lines. The check's queueing is held with the check, and
  said where the check doesn't pass. bump is ship's, so it reads the same.
- **What the lines are made of**: `view.StageLine` and
  `view.UpdateHeading`; the committed subjects are the tidy plan's
  (`engine.TidyPlan.Subjects`, which `tidyrun.Stage` returns in its
  `Tidied`), and the submission's words are submit's (`submit.StageWords`,
  from the `engine.Submitted` that `submit.Apply` now returns).
- Tests that read a stage's full report run ship with `-v`; the others
  read the lines.

### The check's progress, and a batch's rows

- **A stage's check progress is -v's** (`term.Streams.Held`): in ship's
  check stage, the journal's attempt, provider-step, and per-port lines
  aren't printed; on a terminal the latest is the line redrawn, and so is
  "check-1 runs here; Ctrl-C stops it and keeps what finished". A check
  run alone, `dockhand check`, prints them as before; item 4's grammar
  takes check's own output.
- **The checked line says where it passed** (`view.PassedOn`): "✓ checked
  passed on macOS 15 and macOS 26", the check's environments by their
  headings.
- **A batch is a row a change** at the default level: a cell a stage, the
  pull request's number for the submission, `!` or `✗` where it stopped,
  then "Needs you" with why each stopped, where those were standard
  error's lines:

  ```
    jq  ✓ ✓ ✓ #34901
    yq  ✓ ✓ ✓ !
  Needs you
    yq  yq-1.8.1 passed its check and waits for your look, …
  ```

  `ship --mine` and bump's batches read the same. With `-v`, each stage's
  report, as before.

### The six patterns

- **A branch is named once**: update's header was "jq-update ·
  ~/Source/macports-branches/jq-update", after "Started dockhand/jq-update
  from master …" where it started one. It's now one line, "Started … (fetched
  just now) in ~/…", or "jq-update in ~/…", and the directory only where
  the command doesn't run in it (`config.Places.Here`, `view.Where`).
  status's branch detail heads the same way.
- **The checkpoint line** is said where tidy rewrote commits someone might
  want back; a tidy that only committed edits says it at `-v`. tidy has no
  `Next:` to join, so it stays its own line where it's said.
- **The note nudge** ("Note missing; … Add --note") is in a submission's
  plan only where `--note` can still be added: a preview, or a question on
  a terminal (`view.WriteSubmitPlan`'s `noteStill`).
- **The uncommitted-build warning** is one line naming its commits, where
  it was a line a commit. At the default level ship holds the submission's
  plan, so tidy's warning is the one said.
- **The empty upstream comparison** ("Upstream not compared: jq fetches no
  upstream source…") after an edit is `-v`'s; the submission's Upstream row
  says it, and an update's `--plan` still does.
- **submit --plan's description** shows its first 8 lines, then "+N lines
  (dockhand submit --plan -v shows it all)".

### "Said once", as a test

- **Every command test's default output is held to it** (`cmdtest.SaidOnce`,
  which `cmdtest.RunFor` runs after each command line): a pull request's
  URL, a run, or a commit named at most once outside a table, and at most
  one `Next:`. A table's line, its cells set apart by two spaces, names
  what it's about in a column, and a name in a path (a log's
  `~/.dockhand/logs/check-1/…`) is where a file is. It isn't held of `-v`,
  `--json`, `--plan`, help, a question on a terminal, serve's log, status
  `--watch`, or review `--markdown`'s text for pasting; nor of status,
  whose groups item 4 reworks.
- **The width isn't enforced yet**: about 300 lines in the suite are over
  100 columns, from prose that isn't wrapped and table cells that aren't
  cut, which item 4's prose writer and cells bring in.
  `DOCKHAND_TEST_SAID_ONCE_REPORT=<file>` appends both, said-once and width, to a
  file instead of failing, to size what's left.
- **What it found, fixed**:
  - check's "Passed for commit 1a2b3c4." under "checking commit 1a2b3c4":
    "Passed." where the command named what it checks (check, retry, ship);
    wait, which names nothing first, still says it (`checkrun.Report`'s
    `named`).
  - `check --replace`'s "Canceled check-1 before it started." and "check-2
    replaces check-1.": "Replaced check-1, canceled before it started."
  - A baseline's header and rows each named master's commit, and the
    header the check its results name: "jq-update · baseline at master
    1a2b3c4: jq", then "jq · command". check.baseline's own follows the
    failed check's pointer, which named the commit: "jq-update · baseline,
    since check.baseline is on: jq", where "Running a baseline comparison
    because check.baseline is enabled:" was a line of its own.
  - ship's failed check expands its report alone; the plan it was queued
    with is `-v`'s.
  - clean's list of commits it deletes is a table, the commit and its
    subject set apart.
  - review's upstream heading says "the pull request's head" where a
    finding already names that commit.

## Item 4: status and the overview grammar

### status by bucket

- **The buckets are branchstate's** (`branchstate.Bucket`, `BucketOf`):
  Needs you is a branch with a row of `Attend`'s; Done one merged, closed,
  archived, or cleaned; Waiting one whose open pull request asks nothing of
  you, its CI failing or a review asking for changes being a row of
  `Attend`'s; and Working the rest. `branchstate.Working` is what moves a
  Working branch on: `wait` on its check, or `check` where nothing checked
  it yet.
- **status's overview** is the buckets, each a heading with its count, an
  empty one left out:
  - Needs you keeps its rows, their why wrapped and their command below.
    A process that expects another schema is one of its rows, where it was
    a line under the processes table.
  - Working is a line a branch: `⋯`, the branch, its work and its check,
    and its command.
  - Waiting on reviewers is a count with CI's tally, "Waiting on reviewers
    (3): CI passed 2, running 1 · dockhand status -v lists them".
  - `--all` adds "Merged, closed, and archived", a line each.
  - With nothing else, it's one line: "Nothing needs you; 1 pull request
    waits on reviewers (…)".
- **-v** gives the table of every branch, as status gave it before, and
  the checks and processes tables; at the default level, other processes
  are a line, "2 other dockhand processes working on this checkout;
  dockhand status -v lists them".
- **A stopped check's row says its command once**: "check-1 stopped: the
  process running it ended; the command below resumes it, and dockhand
  cancel --branch jq-update ends it instead", where the message named
  `dockhand wait check-1` above the row's own `dockhand wait check-1`.
- status joins the "said once" check.

### The prose writer, and the width enforced

- **Standard output's prose wraps at the width** (`term.wrapWriter`, which
  Begin puts under the output; `term.Wrapped` is one line as it sets it):
  a line wider than the width breaks at its spaces, its later lines two
  past its own indent, so a mark's text lines up under it. A table's line,
  its cells set apart by two spaces or more, isn't wrapped; nor a word with
  nowhere to break, a path or a URL; nor a line a terminal's escape codes
  draw; nor what ends a line a prompt began. The width is a terminal's, or
  `COLUMNS`, else 100.
- **Logs and documents aren't wrapped** (`Streams.Unwrapped`): serve's log,
  `logs`, `diff`, and review `--markdown`'s text for pasting.
- **The width is enforced** in the "said once" check (`cmdtest.Overwidth`):
  a line wider than 100 columns the writer would have wrapped fails the
  test. logs and diff are left out of the check, as serve was.
- **Tests read wrapped words with `cmdtest.Flat`**, which joins each break
  and the indent after it, where the wrap moves with what the line holds,
  as a temporary directory's path; tests of fixed text read the wrapped
  lines.
- The writer writes with `Write`, never `WriteString`, which a writer
  embedding a `bytes.Buffer` takes past its own `Write` (logs `--follow`'s
  test writer, which cancels on what it's written, never saw it).

### check's progress at -v

- **A check's progress is -v's wherever it runs** (`checkrun.quiet`): the
  journal's attempts, record IDs, provider steps, and per-port lines, and
  the notice that it runs here or was handed to serve, are said with `-v`;
  at the default level, on a terminal, the latest is the line redrawn, and
  off one nothing is said until the report. A kept archive found altered
  or gone is still said. wait, which follows a check, reads the same.
- Acceptance row D-I7 runs its checks with `-v`, since it reads "runs here"
  and "attempt 2 of"; the command tests that read progress do too.

### The other overview commands

- **outdated is grouped** by what dockhand can do: "Ready to ship",
  "Held" (a new major version, a release set aside for a look, one not
  ready yet), "Not checked" (with `--current`, or where none could be),
  and with `--current`, "Current"; each a heading with its count, a row a
  port, its versions as "1.7.1 → 1.8.1", and its note. The column headings
  went with the one table; the arrow says what the two versions are.
- **clean says what it did in one line**, "Removed 3 worktrees, 2 Git
  branches, and 2 fork branches; kept 1, which dockhand clean -v lists
  with why" (`engine.CleanKind.Words`), where it listed a line a branch,
  which `-v` still does. Its plan, the question's report, is as it was.
- **setup's rows wrap under their column** (`Streams.Hanging`, which now
  keeps the spaces between a row's cells): the Tart line that ran on
  wraps at the width. The maintainer hint is one sentence in one place
  already, `engine.MaintainerHint`, which create, outdated `--mine`, and
  serve read; setup's `Next:` only names the setting.
- **On a terminal a table's line is cut at the width with "…"**
  (`term.Cut`), at the default level; `-v` shows it whole, and off a
  terminal nothing is cut.
- **status --watch fits the screen**: a view taller than it is cut, its
  last line "+N more; dockhand status shows them all", and since its
  groups come first what needs you, what's cut is what waits
  (`Streams.Height`).
- serve's log was already a line an event, a port's preparing, its check,
  and its pull request each one; it's unwrapped, as a log.

## Item 5: explain for checks and branches

- **`explain check-42`** (the command UX review's §7) says why a check
  ended as it did: each port that failed, where, and at which step, with
  that step's last 8 lines where its provider recorded where its steps
  begin; the test failures its test policy let pass, as advisory; whether
  a baseline can say more, with its command; and the logs to read. A check
  still going says so and names `wait`; one stopped names `retry`; one
  that passed says where.
- **`explain -b <branch>`** (or `-p`, `--pr`) says where a branch stands,
  by its bucket: what needs you there, each hold on its own line, and the
  command that clears it; what it's doing, and its next command; or that it
  waits on reviewers.
- A failed check's `Next:` is `dockhand explain check-42`
  (`refusal.VerbExplain`), where it was `dockhand logs check-42`; explain's
  own `Next:` is the logs. `model.IsRunName` tells a check's name from a
  finding's code.
- README and the usage guide say both.

## Item 6: the shared-flag contract

- **A test over the command tree** (`internal/command/contract_test.go`,
  the command UX review's §9) holds the shared flags to one meaning:
  every runnable command reports with `--json` but the five that say why
  not (open, serve, shell, setup github, setup tart); a command with
  `--plan` and `--yes` takes one or the other, which update, check,
  submit, and tidy now refuse alike, where submit said it its own way
  and the others took both; an edit's `--plan` with no branch to plan in
  plans on master and starts nothing; and a question without a terminal is
  refused alike, exit 1 with what `--yes` would do.
- **--plan without a branch plans on master for every edit**: `checksums
  --plan --new` and `revbump --plan`, with or without `--new`, refused
  where `update --plan` planned on master (field testing's first surface
  item). The engine prepares a rebuild's plan from master as it does a
  checksum refresh's (`updateSource`), and revbump says "Planned on master
  …" as update does.
- **A preview's exit without a terminal** was already one rule,
  `term.Streams.Decide`'s exit 1 (part 4); the test holds it.
- **A check run here keeps to its provider's capacity**, as serve does
  (`engine.awaitSlot`, before it drives a run): checks other live
  processes run count, so two foreground checks don't share one Tart host
  where `providers.tart.capacity` is 1, and the waiting one's journal says
  "check-2 waits for a slot on command: providers.command.capacity is 1,
  and check-1 is using it". The capacity is the engine's
  (`Engine.Capacity`, which compose sets from the configuration), and
  serve reads it there, where it took its own copy in `ServeOptions`.

## After item 6: bump and clean take --plan

- **`bump <port> --plan`** shows ship's own preview of `ship <port>`, its
  row and "Nothing was shipped (--plan).", and changes nothing; **`clean
  --plan`** prints the plan clean shows before it asks, and "Nothing was
  removed (--plan).", as does `clean --delete --plan` for what it would
  forget; `--plan` and `--yes` go apart there too.
- **The contract test names every command without `--plan`**, each with
  why (`noPlan`): what it does is what it'd show, it asks first, or, for
  create and rebase, a plan of its own is Later's, since each is undone
  already. A new command without one fails the test until it says why.
