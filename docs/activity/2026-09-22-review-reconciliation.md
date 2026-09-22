# 2026-09-22: reconciling the second architecture review

A second [architecture review](../reviews/2026-09-22-architecture-and-organization.md)
was written on 2026-09-22 against commit `0f201ebd`, from the code alone,
without the docs, the tests, or the checkout's uncommitted work. This
note checks its findings against the code, against the
[2026-09-21 review](../reviews/2026-09-21-architecture-and-organization.md),
and against the roadmap, and says what changes.

## Verified true

- **Preparation has two owners and they diverge.** A fresh bump prepares
  inside the durable workflow; an update onto an open contribution runs
  `app.prepareOnto`, which resolves the release and edits before
  acceptance and then binds a correction. The review's two examples hold:
  the correction request has no field for patch findings, so the patch
  gate in branch integration sees none for an amendment, and
  `--all-subports` reaches a fresh preparation but is dropped on the
  conversion. The 09-21 review called this "app has become a second
  intake layer" and the roadmap carried it as organization work to do
  after the intake rules stop moving. That was wrong in kind: these are
  two silent losses of a person's choice on a supported path.
- **Git work inside write transactions.** Submission, branch integration,
  and reassociation each call `sharedFiles` inside a `BEGIN IMMEDIATE`
  transaction; every submission with a base runs `git diff-tree` under the
  writer lock, and an edit under `_resources` adds a tree-wide `git grep`.
  That is the [shared-files note](2026-09-21-shared-files-note.md) put
  where the revision is written without regard for the lock.
- **Branch cleanup is implemented twice.** Retention's merged-branch sweep
  re-derives the checked-out, expected-commit, delete, and record
  decisions instead of calling the lifecycle's deletion.
- **`workflow` is 7,171 lines and 165 functions**, the exact count. The
  cycle handles due jobs one after another; verification's execution
  finds its writes with `reflect.DeepEqual` over a shallow clone;
  `preparation.Request` is an alias of the editor's request.
- **The root-keyed global map in `workspace`.** `EnsurePortAt`, `WidenAt`,
  and `ScopeOf` recover a workspace from a root string, which lets code
  holding a tree materialize more of it without saying so. The registry
  landed the same day is the explicit mechanism, and the map is an
  implicit second one; a projection passed with the source is the better
  contract, and the [workspace design](../workspace-design.md) carries it
  as step 5's residue.
- **`sourceInput` concentrates too much and its shallow copy is fragile.**
  The copy is on the roadmap already, and the survey of the same evening
  found a defect that came from exactly that copy.

## Disagreements and qualifications

- **Splitting `workflow` into sibling packages.** The 09-21 position
  stands for `contribution`: adoption, revisions, correction, and
  reassociation share transactions with acceptance, and a sibling would
  need the engine or a fifty-method interface. The review's own test,
  that each owner must receive its real dependencies, decides it:
  `maintenance` passes and is the leaf the 09-21 review named next;
  `contribution` does not.
- **Scheduling.** Correct from the code and unmeasured, as the review
  says. The [instance coordination note](../instance-coordination.md)
  carries the design; the survey's parallelism loss is unrelated to the
  driver loop and is measured first.
- **"Preview should invoke the same service."** It does; preview and the
  fresh path share `preparation.Service`. The divergence is prepare-onto.
- **Leaving `cli` intact** against the 09-21 command pipeline: both are
  gated on the command surface settling; neither is urgent.

## What changes

One item enters Next ahead of the coverage queue, since it carries two
correctness losses and a lock held over subprocesses: one owner for
preparation with the dropped fields as its acceptance test; the
shared-file lookup computed before the transaction; the retention leaf
with branch cleanup consolidated into it; the workspace's global map
replaced by a passed projection; and the archive coverage plan object in
`portedit`, since three functions repeat the same context-and-group
bookkeeping.
