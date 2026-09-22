# Contribution resolution design

Status: specified 2026-09-22, not implemented. Written after the
[second architecture review](reviews/2026-09-22-architecture-and-organization.md)
and its [reconciliation](activity/2026-09-22-review-reconciliation.md),
and after a pressure test of the `app` plan by a second reviewer on a
different model, whose findings are folded in. Every claim about today's
code names its file.

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
	// Intent, Subject, and References are inherited from the prior job or
	// the contribution's commit when the request left them empty.
	Intent     record.EditIntent
	Subject    string
	References []record.Reference
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
// Resolver reads what a selection means. State may be nil: with no
// database there are no records, and every selection is Fresh unless the
// request names a branch to adopt.
type Resolver struct {
	State      state.Store
	Repository record.RepositoryID
	Repo       *git.Repository
	Ports      macports.Reader
	Forge      forge.PullRequests // for the continuation's PR refresh; nil means not re-checked
}

type Request struct {
	Action    record.Action
	Selection macports.Selection
	ChangeID  record.ChangeID
	Branch    string
	// Adopt names a branch to track first.
	Adopt bool
	// Master fetches authoritative master; a Fresh or Continue needs it,
	// an Onto does not, and a nil Master is only for a resolution that
	// must not touch the network, which then refuses Fresh.
	Master   func(context.Context) (record.Source, error)
	Platform record.Platform
	Intent   record.EditIntent
	Subject  string
	References []record.Reference
}

func (r *Resolver) Resolve(ctx context.Context, request Request) (Resolution, error)
```

The steps, in the order `app.BindPreparation` takes them today:

1. With no store, or a selector that is a path rather than a name and no
   change or branch given, the resolution is Fresh from `Master`.
2. Adopt: track the branch first, then resolve as Onto.
3. Look up the open contribution and the newest job of the action that is
   still its current revision or still running, as `PreparationInput`
   does. No contribution: Fresh.
4. A contribution with no such job, or whose job was itself prepared onto
   it (`Spec.Preparation.Correction != nil`): Onto. Source is the current
   revision's; the selection is the contribution's target with the
   request's variants laid over; the subject is the contribution's own
   unless one was given. This is what `bindOnto` computes today and keeps
   computing; the resolution names it.
5. Otherwise fetch master. Unreachable: Continue from the prior job's
   recorded source, `Degraded` set, `Detail` saying master was not
   checked. Reachable: `CheckContinuation` decides; retired means Fresh
   from master with its detail; a stop is the error it raises today.
6. Continue inherits: `SharedRelease` and `KeepOldChecksums` or-ed with
   the request's, `Stub` taken, subject and references taken when the
   request's are empty, the selection from the prior target with the
   request's variants laid over.

`CheckContinuation` stays as it is, called from step 5; its outcomes are
the resolution's `Detail`. `PreparationInput` becomes the lookup inside
step 3 and stops being an entry point.

## Three properties, or it is worse than today

**It answers without a database.** Seven dry-run tests assert that a
preview never creates the state database. `PreparationInput` runs inside a
store view, so the resolver takes a store that may be nil and answers
Fresh, or Onto for an explicit adopt, without one. The read-only opener
that `app` grows, item 1 of the app plan reduced to its useful part,
returns "no database" as a value the resolver is built with.

**It degrades explicitly.** Master unreachable with a prior job is a
Continue with `Degraded` set, never a silent Fresh. The wording is the
existing one: "master not checked (%v); the open contribution is
continued as recorded".

**It is fallible.** Step 5 refreshes the pull request through the forge,
and an unreachable forge leaves the recorded state as the only fact, in
the detail. The resolver is therefore not a pure function of the records,
and nothing caches a resolution across commands.

## What each consumer does

- **Bump, revision bump, checksums.** `app.BindPreparation` becomes: build
  the resolver from the services, resolve, and hand the resolution to
  `Engine.BindPreparation`, which takes a `Resolution` in place of
  `Source`, `Onto`, `ChangeID`, and the inherited fields. The engine's
  binding keeps the stub redirection, the main-port authorization, the
  build resolution, and the destination. `app.Preparation` loses the
  fields the resolution carries and, once provider choice has its own
  home, the rest; the CLI then builds the workflow request.
- **Preview.** Resolves with the store when it exists and without it
  otherwise, and prepares on `Resolution.Source`. Preview then goes onto a
  contribution exactly when a bump would, which closes the divergence the
  second review predicted and the pressure test confirmed. The
  `"Preparing the update onto the open contribution's branch %s"` line
  becomes the resolution's detail; the change of wording is a commit of
  its own.
- **Verify.** The tracked cases resolve: a target, a branch, a change, or
  the current branch by default is a Continue whose `Change` and
  `Revision` are what `BindVerification` checks the captured branch
  against; a branch adopted by name that is tracked is the same. The
  working-tree capture and the untracked-branch capture are not
  resolutions of a contribution and stay outside the value.
- **Adopt.** `AdoptContribution` is the Adopt kind's first half and stays;
  a bump with `--adopt` resolves Adopt, which tracks and then reads as
  Onto, as the CLI does today in two calls.
- **Corrections.** Amend, rebase, and squash select a contribution by
  branch and capture a candidate; they are Onto with a captured candidate,
  and `BindCorrection` may read `Change` and `Revision` from a resolution
  later. Not part of the first landing.

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

- The state database is never created by a dry run.
- The lines tests assert: "Continuing the port's open contribution",
  "lands as an amendment", "fetching authoritative MacPorts master".
- `app.Preview`'s JSON, which carries no tags, and the status and gc
  result shapes.
- The continuation stops: a port already at the version on master, a
  port someone else moved, a retired contribution, each with its wording.
- One open contribution per port; a second branch adopted for a port with
  one is refused as today.

## Sequence

1. Provider choice out of `app`: `buildResolver` behind interfaces for
   the two providers, in a policy package `app` wires. Self-contained; its
   package-internal test moves with it.
2. The `Resolution` value and `Resolver` in `workflow`, with the four
   kinds, the degraded path, and the no-store path each under test, and
   `PreparationInput` folded in. No consumer changes yet.
3. `Engine.BindPreparation` takes a resolution; `app.BindPreparation`
   resolves and hands it over; the continuation tests in `cli` and
   `workflow` are the acceptance test.
4. Preview resolves the same way; the preview wording change is its own
   commit.
5. `BindVerification`'s selector becomes a Continue resolution.
6. Then, and only then, the question whether the four bindings and the
   resolver lift into `workflow/intake` as a leaf that takes the store,
   the repository, the evaluator, and the forge as values and hands the
   engine a finished request. That move is about 1,300 lines and is worth
   making only if the value has made the bindings alike enough that the
   package boundary is obvious; it is not this design's precondition.

## Risks

- Behavior that lives in the merge of a prior job's intent is covered
  only end to end today; step 2 writes the unit tests before step 3
  moves the code.
- The resolver reaches the forge and the network from inside what reads
  like a lookup; the `Master` function and the nil forge make that
  explicit at every call site, and the resolution's `Degraded` field is
  the only way a failure becomes a value.
- A resolution is computed before binding and consumed by it; nothing
  holds a lock between the two, as nothing does today, and the binding's
  own checks, the idle contribution and the open pull request, stay in
  the binding.
