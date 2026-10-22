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
