# Contribution resolution design

Status: specified 2026-09-22, not implemented. Written after the
[second architecture review](reviews/2026-09-22-architecture-and-organization.md)
and its [reconciliation](activity/2026-09-22-review-reconciliation.md),
and after a pressure test of the `app` plan by a second reviewer on a
different model. A second pass by that reviewer attacked this document
scenario by scenario, every bump, revision bump, checksums, and verify
shape through today's code and then through the steps below; the six
gaps it found and their resolutions are in the validation section, and
the steps and the value carry the fixes. Every claim about today's code
names its file.

## The problem

Dockhand answers one question in five places, in five vocabularies: what
does this selection mean, for this action, given what is recorded? The
answer is one of four things. The port is bumped fresh from master. A
prior job of the same action is continued from the source it recorded. An
update is prepared onto an open contribution's own revision, an adopted
branch or one amended by hand. Or a branch dockhand did not make is
adopted. The five places:

- `Engine.PreparationInput` (`internal/workflow/contribution_select.go`)
  finds the open contribution for a port and the newest job of the action
  that is still its current revision or still running.
- `Engine.CheckContinuation` (`internal/workflow/continuation.go`) decides
  whether a prior job continues or the contribution is retired, by
  evaluating the port on master and refreshing the pull request.
- `app.BindPreparation` (`internal/app/preparation.go`) decides between
  onto, continue, and fresh from those two, fetches master, degrades when
  master is unreachable, and merges the prior job's edit intent, subject,
  references, and variants into the request.
- `app.PreviewPreparation` finds an open contribution with its own
  `openContributionSource`, which goes onto any open contribution, where
  the real bump goes onto one only when it has no bump job of its own.
- `app.BindVerification` (`internal/app/verification.go`) picks the
  contribution a verification continues: by target, branch, or change,
  the current branch by default, and a branch adopted by name that turns
  out to be tracked.

The workspace design found six names for one value and made it one type.
This is the same shape: the value is the resolution, and the five places
each compute part of it and hand the rest on by convention.

## The design in one paragraph

A resolution is what one selection means for one action: its kind, fresh,
continue, onto, or adopt; the source the edit or the capture runs on; the
contribution and revision it lands on, when there is one; the choices
inherited from a prior job; and one line saying how it was reached. The
engine computes it once, from the records, the repository, and a way to
fetch master, and every binding consumes it: a bump binds a preparation
from it, a verification binds its continuation from it, a preview reads
its source from it, and adoption is its fourth kind. It answers without a
database, so a dry run stays a dry run; it degrades explicitly when master
is unreachable, keeping the recorded source and saying so; and it is
fallible, because the continuation check refreshes the pull request from
the forge. It is a value with a reason, not a pure function.

## The value

```go
// Kind is what a selection resolves to.
type Kind string

const (
	// Fresh starts from master: no open contribution, or one retired by
	// what master and the forge showed.
	Fresh Kind = "fresh"
	// Continue continues a prior job of the action from the source it
	// recorded, with its choices inherited.
	Continue Kind = "continue"
	// Onto prepares onto an open contribution's current revision and
	// replaces its branch head: a contribution with no bump job of its
	// own, or whose last bump was itself prepared onto it.
	Onto Kind = "onto"
	// Adopt tracks a branch dockhand did not make, then resolves Onto.
	Adopt Kind = "adopt"
)

// Resolution is what one selection means for one action.
type Resolution struct {
	Kind Kind
	// Source is what the edit runs on: master, the prior job's recorded
	// source, or the contribution's current revision.
	Source record.Source
	// Master is master as fetched, when it was; a Continue reached with
	// master unreachable has none, and Degraded says why.
	Master   *record.Source
	Degraded string
	// Change and Revision are the contribution and revision the work
	// lands on, for Continue, Onto, and Adopt.
	Change   *record.Change
	Revision *record.Revision
	// Prior is the job a Continue inherits from.
	Prior *record.Job
	// Selection is the target as the records name it, with the person's
	// variant choices laid over the recorded ones.
	Selection macports.Selection
	// Intent, Subject, and References are the request's; a Continue
	// inherits all three from the prior job where the request left them
	// empty, an Onto inherits only the subject, from the contribution's
	// commit, as today, and a Fresh inherits nothing.
	Intent     record.EditIntent
	Subject    string
	References []record.Reference
	// Branch is what the work lands on: master's name for a Fresh, the
	// contribution's branch otherwise. A preview reports it.
	Branch string
	// Checked is whether master and the pull request were consulted for a
	// Continue; a preview does not consult them, and says so in Detail.
	Checked bool
	// Detail is the one line a command reports: what master holds, what
	// the pull request is, why the contribution continues or retires.
	Detail string
}
```

The kind and the source are the facts; the rest is what today's callers
each re-derive from them. `Degraded` is the one field that carries a
failure as a value, and it is only ever set with `Kind == Continue`: an
unreachable master with a prior job continues from the recorded source,
as `app.BindPreparation` does today, and the line says master was not
checked. An unreachable master with no prior job is an error, with the
text the preparation tests assert.

## How it is computed

```go
// ResolutionRequest is the selection as the person made it.
type ResolutionRequest struct {
	Action    record.Action
	Selection macports.Selection
	ChangeID  record.ChangeID
	Branch    string
	// Adopt names a branch to track first; with Preview it is tracked in
	// a dry run and the resolution reads the revision adoption would have
	// recorded, as the CLI's dry-run adopt does today.
	Adopt bool
	// Require refuses a selection with no open contribution rather than
	// resolving Fresh: a verification continues something or says so.
	Require bool
	// Lookup stops after step 4: the contribution and the prior job are
	// read, master is not fetched and the continuation is not checked. A
	// verification resolves this way; its action has no release to check
	// master against.
	Lookup bool
	// Preview reads and never writes: master is fetched, the pull request
	// is not refreshed and the continuation is not checked, and nothing
	// is recorded. A Continue found this way is unchecked, and says so.
	Preview bool
	// Offline forbids the master fetch: a resolution that would need it,
	// Fresh or a Continue that checks master, is refused rather than made.
	Offline    bool
	Platform   record.Platform
	Intent     record.EditIntent
	Subject    string
	References []record.Reference
}

// Resolve is an engine method: the engine has the store, the repository,
// the evaluator, and the forge already, and a separate resolver would be
// the engine's dependency set under a second name, the layering the
// second review objected to in app. The one rule this method adds is
// that a nil State means no records: every selection is Fresh, or Onto
// for an explicit adopt, and nothing is read. A preview builds an engine
// without a store the way it builds one without providers.
func (e *Engine) Resolve(ctx context.Context, request ResolutionRequest) (Resolution, error)
```

The engine fetches master itself when a resolution needs it, through its
repository and the ports repository constants, through `Engine.FetchMaster`,
which adoption calls too; `Offline` is the only control, and it is a refusal, not a
degradation. The degraded path is a fetch that was attempted and failed.

The steps, in the order `app.BindPreparation` takes them today, with
adoption first because it is refused for a path selector today and must
not be silently skipped by one:

1. Adopt: track the branch first, refusing a path selector as adoption
   does, in a dry run under `Preview`; then resolve as Onto against the
   recorded revision, or the one the dry run would have recorded.
2. With no store, or a selector that is a path rather than a name and no
   change or branch given, the resolution is Fresh from master. A nil
   store with `Adopt` is an error: adoption needs the store.
3. Look up the open contribution and the newest job of the action that is
   still its current revision or still running, as `PreparationInput`
   does. No contribution: Fresh, or the no-contribution error under
   `Require`, with the wording verification gives today.
4. A contribution with no such job, or whose job was itself prepared onto
   it (`Spec.Preparation.Correction != nil`): Onto. Source is the current
   revision's; the selection is the contribution's target with the
   request's variants laid over; the subject is the contribution's own
   unless one was given. This is what the onto binding computes today and keeps
   computing; the resolution names it.
5. Under `Lookup`, stop: the prior job is a Continue as found. Otherwise
   fetch master, unless `Offline`, which refuses here. Unreachable:
   Continue from the prior job's recorded source, `Degraded` set,
   `Detail` saying master was not checked. Reachable: under `Preview`,
   Continue unchecked, since the continuation check refreshes the pull
   request and writes; otherwise `CheckContinuation` decides; retired
   means Fresh from master with its detail; a stop is the error it raises
   today.
6. Continue inherits: `SharedRelease` and `KeepOldChecksums` or-ed with
   the request's, `Stub` taken, subject and references taken when the
   request's are empty, the selection from the prior target with the
   request's variants laid over.

`CheckContinuation` stays as it is, called from step 5; its outcomes are
the resolution's `Detail`. `PreparationInput` becomes the lookup inside
step 3 and stops being an entry point.

## Three properties, or it is worse than today

**It answers without a database, and never writes for a preview.** Seven
dry-run tests assert that a preview never creates the state database.
`PreparationInput` runs inside a store view, so `Resolve` is the one
engine method that accepts a nil store, and answers Fresh without one;
adoption needs the store and is refused without it. The read-only opener
that `app` grows, item 1 of the app plan reduced to its useful part,
returns "no database" as a value the engine is built with. With a
database, a preview reads it and writes nothing: `Preview` skips the
continuation check, because `CheckContinuation` refreshes the pull
request through `refreshChange`, which records what it observed with
`State.Update` (`internal/workflow/contribution_lifecycle.go`) and defers
a failed observation the same way. A resolution that wrote during a dry
run would break the promise the command's help makes.

**It degrades explicitly.** Master unreachable with a prior job is a
Continue with `Degraded` set, never a silent Fresh. The wording is the
existing one: "master not checked (%v); the open contribution is
continued as recorded".

**It is fallible.** Step 5 refreshes the pull request through the forge,
and an unreachable forge leaves the recorded state as the only fact, in
the detail. The resolution is therefore not a pure function of the
records, and nothing caches one across commands.

## What each consumer does

- **Bump, revision bump, checksums.** `app.BindPreparation` becomes:
  resolve through the engine, then hand the resolution to
  `Engine.BindPreparation`, which takes a `Resolution` in place of
  `Source`, `Onto`, `ChangeID`, and the inherited fields. The engine's
  binding keeps the stub redirection, the main-port authorization, the
  build resolution, and the destination. `app.Preparation` loses the
  fields the resolution carries and, once provider choice has its own
  home, the rest; the CLI then builds the workflow request.
- **Preview.** Resolves under `Preview`, with the store when it exists
  and without it otherwise, and prepares on `Resolution.Source`. Preview
  then goes onto a contribution exactly when a bump would, and continues
  a prior job from its recorded source exactly when a bump would, which
  closes the divergence the second review predicted and the pressure test
  confirmed. That is a user-visible change and a deliberate one: today a
  preview of a port mid-bump edits the branch head, while the bump that
  follows edits the recorded source, so the preview shows a diff the
  bump will not make. The `"Preparing the update onto the open
  contribution's branch %s"` line becomes the resolution's detail, and a
  test covers a preview onto a contribution, which nothing does today.
  `Resolution.Branch` is what `Preview.Branch` reports, so its JSON is
  unchanged.
- **Verify.** The tracked cases resolve under `Lookup` and `Require`: a
  target, a branch, a change, or the current branch by default, which the
  request fills before resolving as `app.BindVerification` does today, is
  a Continue whose `Change` and `Revision` are what `BindVerification`
  checks the captured branch against, and no contribution is the error
  with today's wording and the `--adopt` hint. A branch adopted by name
  that is tracked is the same only when a port name was given, the guard
  `app.BindVerification` keeps today; without one the branch is an
  untracked capture, and the value does not change that. The working-tree
  capture and the untracked-branch capture are not resolutions of a
  contribution and stay outside the value.
- **Adopt.** `AdoptContribution` is the Adopt kind's first half and stays;
  a bump with `--adopt` resolves Adopt, which tracks and then reads as
  Onto, as the CLI does today in two calls.
- **Corrections.** Amend, rebase, and squash select a contribution by
  branch and capture a candidate; they are Onto with a captured candidate,
  and `BindCorrection` may read `Change` and `Revision` from a resolution
  later. Not part of the first landing, and the validation is right that
  this leaves the lookup of a contribution by name or branch written a
  third time; step 3's lookup is the one `BindCorrection` reuses when it
  follows, and the sequence says when.

## What it replaces

`app.PreparationInput`'s use and `Engine.PreparationInput` as an entry
point; `openContributionSource`; the onto, continue, and fresh decision
and the intent merge in `app.BindPreparation`; the continuation selector
in `app.BindVerification`; and the `Onto` field that `PreparationRequest`
gained on 2026-09-22, which becomes the `Onto` kind of the resolution it
takes instead.

## What it does not cover

Provider choice, `app.buildResolver`, which is verification policy over
two providers and gets its own object before this lands, since the
request fields that feed it are the ones `app.Preparation` would
otherwise keep. Publication destination, resolved at binding from the
publish options. Garbage collection, which is composition and stays in
`app`. A single request vocabulary across cli, app, and workflow: the
`preparation.Request` alias of the editor's request blocks it, and the
resolution removes the translation that mattered without it.

## What must not change

- The state database is never created or written by a dry run, except
  that `--dry-run --adopt` builds the services and so creates the
  database today, an inconsistency the roadmap carries; this design does
  not widen it.
- The lines tests assert: "Continuing the port's open contribution",
  "selector does not match contribution", "lands as an amendment",
  "fetching authoritative MacPorts master", "no open contribution for"
  with its `--adopt` hint, "tracked" from adoption, and "branch master"
  in a verbose preview.
- `app.Preview`'s JSON, which carries no tags, and the status and gc
  result shapes.
- The continuation stops, each with its wording: a port already at the
  version on master, a port someone else moved, a retired contribution
  whose pull request is neither open nor merged, a port no longer on
  master, and a port that could not be evaluated on master; and their
  exit code, which the CLI maps from `ErrContinuation`.
- The flag exclusions that keep some combinations from arising:
  `--change` with `--dry-run`, and `--change` with `--adopt`.
- The revision-bump subject rule: a subject is required unless the update
  goes onto a contribution, which then supplies its own. The resolution
  carries the subject; the rule reads the kind.
- One open contribution per port; a second branch adopted for a port with
  one is refused as today.

## Validation

The scenario pass found six gaps. Each is resolved above; this is the
record of what and why.

1. A verification with no contribution is an error today, and a
   resolution that answered Fresh could not say so: `Require` refuses
   with today's wording. Verification also has no release to check
   master against, and a prior verify job has no resolved release for
   `CheckContinuation` to read: `Lookup` stops after the lookup.
2. A preview that resolved as a bump does would fetch master and run the
   continuation check, whose pull request refresh writes: `Preview`
   fetches and does not check, and a Continue found that way is
   unchecked, in `Checked` and in the detail. The preview of a port
   mid-bump changes from the branch head to the recorded source, which is
   what the bump edits; that is the divergence closed, and it lands with
   its own test.
3. `Preview.Branch` is reported JSON with nothing to read it from:
   `Resolution.Branch`.
4. The value's doc read as if an Onto inherited the prior job's intent;
   it inherits only the contribution's subject, as today. The pass also
   found that today's Onto therefore drops a stub's redirection and its
   shared-release authorization, so a checksums refresh or a second bump
   onto an adopted `py-foo` contribution edits the carrier without
   authorizing its siblings; that is a defect in the current code, not a
   property to preserve, and the roadmap carries it.
5. A dry-run adopt had no representation: `Adopt` under `Preview` is the
   dry-run adoption the CLI performs today, and the resolution reads the
   revision it would have recorded.
6. The path-selector shortcut preceded adoption, so `--adopt` with a path
   selector would have been ignored where today it is refused: adoption
   is step 1.

The pass also lengthened the list of what must not change, above, and
noted that the promise "a dry run touches no database" is already broken
by `--dry-run --adopt`, which builds the services; the roadmap carries
that too.

## Sequence

1. Provider choice out of `app`: `buildResolver` behind interfaces for
   the two providers, in a policy package `app` wires. Landed 2026-09-22
   as `workflow/choice` ([note](activity/2026-09-22-provider-choice.md)).
2. The `Resolution` value and `Engine.Resolve` in `workflow`, with the
   four kinds, the degraded path, the offline refusal, and the no-store
   path each under test, and `PreparationInput` folded in. No consumer
   changes yet. Landed 2026-09-22.
3. `Engine.BindPreparation` takes a resolution; `app.BindPreparation`
   resolves and hands it over; the continuation tests in `cli` and
   `workflow` are the acceptance test. Landed 2026-09-22.
4. Preview resolves under `Preview`; the change to what a preview of a
   port mid-bump shows, and its wording, is its own commit with its own
   test. Landed 2026-09-22.
5. `BindVerification`'s selector becomes a Continue resolution under
   `Lookup` and `Require`, with the current-branch default filled before
   resolving. Landed 2026-09-22: an action that prepares no update
   resolves to the contribution as recorded, so the two flags are
   implied for it.
6. `BindCorrection` reuses step 3's lookup for its contribution. Landed
   2026-09-22; the five landings are in one
   [note](activity/2026-09-22-resolution-landed.md).
7. Then, and only then, the question whether the four bindings and
   `Resolve` lift into `workflow/intake` as a leaf that takes the store,
   the repository, the evaluator, and the forge as values and hands the
   engine a finished request. That move is about 1,300 lines and is worth
   making only if the value has made the bindings alike enough that the
   package boundary is obvious; it is not this design's precondition.
   Answered no on 2026-09-22, from the code after step 6
   ([note](activity/2026-09-22-no-intake-leaf.md)): the set is 2,223
   lines that call nine engine helpers, one of them the pull request
   refresh that writes state and reaches the forge, and that the
   acceptance side calls back into for the request type, the branch
   adoption, the correction's transactional checks, and the contribution
   lookup; the bindings read alike at their prologues and their result
   and nowhere in between; and "intake" already names the submit
   transaction in the architecture. The 2026-09-16 review's condition
   for the split, that the shared pieces move cleanly, is not met.

## Tightened after the follow-up review, 2026-09-22

The [follow-up review](reviews/2026-09-22-architecture-follow-up.md) read
the value after step 6 and asked for its contract to be tightened before
more callers depend on it. Three changes, all landed the same day
([note](activity/2026-09-22-contribution-revision-findings.md)), and
they amend what the sections above say:

- `Require`, `Lookup`, and `Offline` are gone. An action that prepares no
  update, a verification or a correction, resolves to the contribution as
  recorded whatever else is asked, so the first two were implied by the
  action, and the third had no caller. `Preview` is the one control.
- A fifth kind, `Tracked`: the contribution as recorded with its current
  revision, what a verification or a correction works on. It decides
  nothing about master or a prior job, and `Continue` means only what
  step 2 said: a prior job's recorded input and inherited choices. The
  verification binding takes a `Tracked` resolution and nothing else.
- Adoption is not a mode of `Resolve`. The caller adopts first, through
  `app.Adopt`, in a dry run for a preview, and `Engine.ResolveAdopted`
  reads the result into the Adopt kind. `Resolve` reads and never writes.

## Risks

- Behavior that lives in the merge of a prior job's intent is covered
  only end to end today; step 2 writes the unit tests before step 3
  moves the code.
- `Resolve` reaches the forge and the network from inside what reads like
  a lookup; `Offline` and a nil forge make that explicit at the call
  site, and the resolution's `Degraded` field is the only way a failure
  becomes a value.
- A method that accepts a nil store on an engine whose every other method
  refuses one is a rule to state once and test, not a convention; the
  test is a preview resolved against no database.
- A resolution is computed before binding and consumed by it; nothing
  holds a lock between the two, as nothing does today, and the binding's
  own checks, the idle contribution and the open pull request, stay in
  the binding.
