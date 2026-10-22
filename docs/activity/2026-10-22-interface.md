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
