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
