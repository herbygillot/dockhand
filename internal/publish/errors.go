package publish

import (
	"errors"
	"fmt"
	"strings"

	"github.com/herbygillot/dockhand/internal/record"
)

// The refusals of the publication lifecycle, in one file because they
// are one ladder.
//
// AN ERROR BELONGS TO THE OWNER OF WHAT IT REFUSES. The blocked,
// canceled, superseded, queued, unsupported and errored refusals are run
// outcomes and belong to run; only the publication refusals belong here,
// which is the rule the shipped internal/verdict did not state and which
// is why its refusal.go held six kinds of thing at once.
//
// NONE OF THEM CARRIES AN EXIT BAND, and that is the same choice
// internal/run, internal/change and internal/lease already made: a
// lifecycle states what it refused and cli maps identities to exit
// codes. The shipped types implemented DockhandExit and Code here, which
// put the command line's numbering inside a package the command line
// composes.
//
// A refusal that carries DATA a caller must act on is a typed struct
// wrapping its sentinel with %w — the other pull request's number and
// URL, the size a body overran by, the regions that refused a machine —
// because a caller that has to re-derive those from a sentence cannot
// offer them, and reading them back out of prose is rule 6's
// prohibition. A refusal whose whole content is its identity is a bare
// sentinel.

var (
	// ErrStale is Apply refusing to act on a permission that has gone out
	// of date: the branch moved, or the pull request the permit was
	// granted over is no longer in the state Authorize weighed.
	ErrStale = errors.New("publish: the branch moved since it was authorized")

	// ErrNotFresh is Authorize refusing Facts whose Forge.Fresh is false: a
	// cached standing never reaches a decision.
	ErrNotFresh = errors.New("publish: the forge facts were not asked for by this gather")

	// ErrPaceSpent is the machine road refusing because Spent has reached
	// pace.Max inside pace.Window. A person is never refused on it.
	ErrPaceSpent = errors.New("publish: the machine's publication allowance is spent")

	// ErrPaceUnset is the machine road refusing a Pace nobody chose (rule
	// 7): a zero Pace on the road that publishes by default is a wiring gap,
	// not "publish nothing" and not "publish freely". Promote passes a zero
	// Pace and is never asked, because a person is not paced.
	ErrPaceUnset = errors.New("publish: no publication pace was chosen for the machine")

	// ErrSpendUnknown is the machine road refusing a Spend nobody counted.
	// It is Spend.Counted's refusal and it exists for the reason the type
	// has an unexported flag at all: a spend that reads zero because
	// nobody derived it admits the whole cap, which is the one direction
	// this value must never fail in.
	ErrSpendUnknown = errors.New("publish: the machine's spend was never counted over the store")

	// ErrForgePolicyUnset is Gather refusing a ForgePolicy nobody chose.
	// Rule 7 again, one stage earlier: the difference between asking the
	// forge and serving what a cycle cached is a decision a caller makes,
	// and a zero that quietly meant either would put a cached standing in
	// front of a gate that must never see one.
	ErrForgePolicyUnset = errors.New("publish: no forge policy was chosen for this gather")

	// ErrNoPermit is Apply refusing a Permit that Authorize did not
	// return. It is the last hole in "Apply is uncallable without a
	// permit": unexported fields stop a caller from inventing a permit
	// with steps in it, and nothing in Go stops `publish.Permit{}`.
	ErrNoPermit = errors.New("publish: this permit was not granted by Authorize")

	// ErrForgeSilent is the machine road refusing a question the forge did
	// not answer. The human road downgrades the same silence to an
	// advisory and the comment in the shipped tree names its own
	// condition: the checklist box is left for the human. There is no
	// human. Worse than advisory, an own-PR lookup that failed reads as
	// "this branch has no pull request", which would make an unattended
	// pass open a SECOND one against a queue that already has the first.
	ErrForgeSilent = errors.New("publish: the forge did not answer, and an unattended publication will not guess")

	// ErrDuplicate is an open upstream pull request already proposing this
	// change: a duplicate spends reviewer attention on the purest kind of
	// waste. Refusal with a remedy — the other pull request may be theirs
	// to join, --title retitles, --no-pr-check publishes past it
	// deliberately — and never a failure.
	ErrDuplicate = errors.New("publish: an open pull request already proposes this change")

	// ErrMerged is a publication whose own pull request already merged:
	// there is nothing left to publish, and pushing to that branch would
	// resurrect work the project has already taken. A dead end on BOTH
	// roads, which is why it is not a machine-only rung.
	ErrMerged = errors.New("publish: this branch's pull request already merged")

	// ErrFailed is the evidence gate's one refusal on the human road: a
	// verification that RAN TO COMPLETION and said the port does not
	// build. --ignore is the deliberate override, and it turns this into
	// an advisory the pull request body states rather than skipping
	// anything.
	ErrFailed = errors.New("publish: the tip has a failed verification")

	// ErrPending is the machine's answer to a change whose verification
	// has not finished: nothing is wrong, nothing is settled, and the next
	// pass will ask again. It is NOT a refusal a person ever meets — a
	// person publishing mid-build has thereby answered the question — and
	// a caller that read it as a refusal would page somebody about work
	// proceeding exactly as it should.
	ErrPending = errors.New("publish: verification has not finished")

	// ErrUnproven is the machine's inversion of the human road's most
	// permissive rule. A person invoking promote has already made the
	// publication choice, so an unverified branch goes out with a
	// complaint: they are looking at the complaint. An unattended pass has
	// nobody looking, and absence of evidence there is not a candour
	// problem but the absence of any reason to spend a reviewer's
	// attention at all. The machine road requires POSITIVE evidence.
	ErrUnproven = errors.New("publish: an unattended publication needs a passing verification, not the absence of a failure")

	// ErrNoGrant is a machine on a build, or against a change, that grants
	// it nothing. It is named for the permission and not for the
	// withholding, which is Grant's whole reason for replacing a bool.
	ErrNoGrant = errors.New("publish: this machine is granted no publication")

	// ErrProposalOpen is the machine refusing while a finding nobody has
	// answered still stands on the change. A person is told and allowed
	// through — they are the person the question was for — and there is
	// nobody on the unattended road to have read it.
	ErrProposalOpen = errors.New("publish: a finding on this change is still unanswered")

	// ErrNotSimple is the machine refusing a change whose edits are not
	// confined to the version, checksum and vendored regions, or which
	// could not be reconstructed and judged at all (change.Unjudged, which
	// withholds by rule 7).
	ErrNotSimple = errors.New("publish: this change is not one a machine may publish unattended")

	// ErrDirectionUnknown is the machine refusing a change whose version
	// movement could not be compared. "I could not find out" is not "it
	// moves forward", and a machine that published on the second reading
	// of the first answer would be the exact defect Direction exists to
	// close.
	ErrDirectionUnknown = errors.New("publish: this change's version movement could not be compared")

	// ErrEpochOwed is the machine refusing a downgrade base would decline
	// to install: the version string moved and macports.Move says an
	// install at the old version would not be upgraded to the new one. The
	// human road emits the epoch edit and says so; the machine road stops.
	ErrEpochOwed = errors.New("publish: this change moves the version backwards and owes an epoch bump")

	// ErrDrifted is the machine refusing a change whose base has moved
	// underneath it — an upstream commit touched this change's own portdir
	// since it was cut — or one whose drift could not be measured at all.
	// A person is advised and publishes anyway if they mean to.
	ErrDrifted = errors.New("publish: the tree moved under this change since it was cut")

	// ErrBodyTooLong is a body GitHub will not take, refused before
	// anything leaves the machine. IT DOES NOT TRUNCATE, and that is the
	// decision rather than an omission: the member lines are the evidence
	// a reviewer is being asked to accept, and a body that omits some of
	// them is vouching for what it does not show. The remedy is a smaller
	// change.
	ErrBodyTooLong = errors.New("publish: the pull request body is longer than the forge will take")

	// ErrNoRow is a mutator addressing a publication the store does not
	// hold — compacted, or a state ref recreated under a pass.
	ErrNoRow = errors.New("publish: the store holds no publication of that id")

	// ErrNotSettled is DeleteForkIn refusing a publication the forge has
	// not finished with: the fork copy of an open pull request is the head
	// the review is reading.
	ErrNotSettled = errors.New("publish: the publication is not settled")

	// ErrAlreadySettled is RetireIn refusing to write a second terminal
	// outcome. A publication ends once; a second write would be a silent
	// rewrite of what the forge said the first time.
	ErrAlreadySettled = errors.New("publish: the publication is already settled")

	// ErrNotTerminal is RetireIn refusing an outcome that is not one:
	// record.Open is the state a row is already in, and writing it as a
	// retirement would close nothing while claiming to.
	ErrNotTerminal = errors.New("publish: that is not a terminal outcome")

	// ErrForkStepStands is DeleteForkIn refusing a publication that
	// already carries a DeleteFork step. One deletion is recorded once;
	// the retry lives on the step's own Attempt and NotBefore, which is
	// what ForkGoneIn writes.
	ErrForkStepStands = errors.New("publish: the publication already carries a fork deletion")

	// ErrNoForkStep is ForkGoneIn refusing a publication with no Requested
	// or Uncertain DeleteFork step: there is no obligation to record an
	// outcome against, and writing one would invent a deletion nobody
	// asked for.
	ErrNoForkStep = errors.New("publish: the publication carries no outstanding fork deletion")

	// ErrNotAnOutcome is ForkGoneIn refusing a phase that is not one of
	// the three this step can end in. Requested means the copy still
	// stands (retry on a backoff), Finished that it is gone, Uncertain
	// that the remote could not be asked; Acquiring and Active are the
	// lease lifecycle's words and mean nothing here.
	ErrNotAnOutcome = errors.New("publish: that is not an outcome a fork deletion can have")

	// ErrNoChange is Gather meeting a resolved change the store does not
	// hold. Resolve reads the store, so this is a peer that closed and
	// compacted the record in the window between — reported rather than
	// guessed past.
	ErrNoChange = errors.New("publish: the store holds no record of this change")

	// ErrNoInvoker is Authorize refusing Facts that do not say who is
	// asking. The invoker is an INPUT and a per-operation constant — Human
	// on promote and on a person's cycle, Machine on dispatch — set by the
	// road and never inferred from a terminal, and with --auto retired
	// there is no ambient value it could fall back to.
	//
	// It matters because the zero would be read as a person, which is the
	// permissive road: --ignore and --no-pr-check would be honoured, the
	// grant would never be asked, the pace would never be counted and the
	// confinement verdict would never be weighed. Every one of those is a
	// gate that exists for the unattended road, and a forgotten field is
	// exactly how a machine comes to walk the human one.
	ErrNoInvoker = errors.New("publish: no invoker was declared for this publication")

	// ErrNoEnv is Gather with no repository or no store to read. It is an
	// incident and not a fact: there is nothing to gather from, so there
	// is nothing for a road to decide about, and a caller that met an
	// empty Facts instead would decide over zeros.
	ErrNoEnv = errors.New("publish: this road was wired with no repository or no store")

	// ErrNoBranch is Gather meeting a change with no branch. A publication
	// pushes a branch to a fork; a branchless snapshot has nothing to
	// push, and its verification is the whole of what it was for.
	ErrNoBranch = errors.New("publish: the change has no branch to publish")
)

// DuplicateError is ErrDuplicate typed: the pull request that already
// proposes this change, so a caller can offer it rather than describe
// it. Both fields are the answer.
type DuplicateError struct {
	Number int
	Title  string
	URL    string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("an open PR already proposes %q: %s — join it, retitle with --title, or --no-pr-check to publish anyway",
		e.Title, e.URL)
}

func (e *DuplicateError) Unwrap() error { return ErrDuplicate }

// MergedError is ErrMerged typed: the pull request that already landed.
type MergedError struct {
	Number int
	Branch string
	URL    string
}

func (e *MergedError) Error() string {
	return fmt.Sprintf("PR #%d for %s already merged (%s) — `dockhand cycle` retires the branch",
		e.Number, e.Branch, e.URL)
}

func (e *MergedError) Unwrap() error { return ErrMerged }

// FailedError is ErrFailed typed with the platforms whose builds said
// no. Named rather than counted: which build failed is something a
// reader can look up, and "1 run" is not.
type FailedError struct {
	Branch    string
	Tip       string
	Platforms []string
}

func (e *FailedError) Error() string {
	return fmt.Sprintf("%s: tip %s has a failed verification (%s) — fix it, `dockhand discard` it, or --ignore to publish anyway",
		e.Branch, e.Tip, strings.Join(e.Platforms, ", "))
}

func (e *FailedError) Unwrap() error { return ErrFailed }

// PendingError is ErrPending typed with the platforms whose runs have
// not finished, in the change's own stable order.
type PendingError struct {
	Branch    string
	Platforms []string
}

func (e *PendingError) Error() string {
	return fmt.Sprintf("%s: verification has not finished (%s); the next pass will ask again",
		e.Branch, strings.Join(e.Platforms, ", "))
}

func (e *PendingError) Unwrap() error { return ErrPending }

// NotSimpleError is ErrNotSimple typed with change.Judge's own reasons —
// the regions that refused it, in the words the judge produced. It
// carries them rather than restating them because the pass that refuses
// is unattended: the only account anybody ever gets is what is written
// down.
type NotSimpleError struct {
	Branch string
	Why    []string
}

func (e *NotSimpleError) Error() string {
	if len(e.Why) == 0 {
		return fmt.Sprintf("%s: this change could not be reconstructed and judged, so no machine may publish it", e.Branch)
	}
	return fmt.Sprintf("%s: a machine may publish only a verified version bump confined to the version, checksum and vendored regions — %s",
		e.Branch, strings.Join(e.Why, "; "))
}

func (e *NotSimpleError) Unwrap() error { return ErrNotSimple }

// BodyTooLongError is ErrBodyTooLong typed with the two numbers, in the
// unit the forge's own refusal is written in: characters, which is code
// points and not bytes. The difference is real on this template — every
// evidence line carries an em-dash — and a count in bytes would refuse a
// body GitHub takes.
type BodyTooLongError struct {
	Branch string
	Size   int
	Limit  int
}

func (e *BodyTooLongError) Error() string {
	return fmt.Sprintf("%s: the pull request body is %d characters and the forge takes at most %d — nothing was pushed. The body is not trimmed to fit, because its member lines are what a reviewer is asked to accept; the remedy is a smaller change",
		e.Branch, e.Size, e.Limit)
}

func (e *BodyTooLongError) Unwrap() error { return ErrBodyTooLong }

// ForgeSilentError is ErrForgeSilent typed with the question that went
// unanswered — this branch's own pull request, the same-port duplicate
// search — so a reader knows which answer is missing rather than only
// that one is.
type ForgeSilentError struct {
	Branch string
	What   string
	Err    error
}

func (e *ForgeSilentError) Error() string {
	return fmt.Sprintf("%s: could not ask the forge about %s (%v); an unattended publication will not guess, and the next pass will ask again",
		e.Branch, e.What, e.Err)
}

// Unwrap returns both the identity a caller branches on and the cause
// underneath it, so errors.Is finds ErrForgeSilent and a reader still
// meets whatever the forge actually did.
func (e *ForgeSilentError) Unwrap() []error { return []error{ErrForgeSilent, e.Err} }

// ErrStepPartial is the identity of a publication that got HALFWAY: at
// least one authorized step completed and a later one did not. It is a
// sentinel beside StepError for the reason every other pair here is one
// — a caller may ask errors.Is without knowing the type.
//
// It carries no exit band, like everything else in this file. What a
// half-done publication MEANS to a process is the command line's to
// number, and it numbers it out of the two facts StepError carries: a
// branch pushed whose pull request would not open and one whose pull
// request would not refresh are different codes, and neither may be
// folded into the band of last resort, because a wrapper reading "1"
// re-runs and pushes a second time.
var ErrStepPartial = errors.New("publish: a publication step failed after an earlier one completed")

// StepError is a failed step WITH what already stands, which is the
// whole of why it exists: publish.Apply records each step before
// attempting it and marks a failure Uncertain, so the store knows what
// happened, but the error travelling up to a shell used to be the raw
// `gh` or `git` failure and a caller had no way to tell "the branch is
// on the fork and the pull request is not" from "nothing left this
// machine".
//
// Kind is the step that failed and Completed is what finished before it,
// in the permit's order. Both are DATA and not prose: rule 6 forbids a
// caller recovering either by reading the words underneath, which are
// whatever the forge's cli chose to print.
type StepError struct {
	Kind      record.StepKind
	Completed []record.StepKind
	Err       error
}

func (e *StepError) Error() string {
	if len(e.Completed) == 0 {
		return fmt.Sprintf("publish: %s failed and nothing was completed: %v", e.Kind, e.Err)
	}
	done := make([]string, 0, len(e.Completed))
	for _, k := range e.Completed {
		done = append(done, string(k))
	}
	return fmt.Sprintf("publish: %s failed after %s completed — that half stands: %v",
		e.Kind, strings.Join(done, ", "), e.Err)
}

// Did reports that a step of this kind completed before the failure.
func (e *StepError) Did(k record.StepKind) bool {
	for _, done := range e.Completed {
		if done == k {
			return true
		}
	}
	return false
}

// Unwrap returns the identity a caller branches on and the cause
// underneath it, so errors.Is finds ErrStepPartial and a reader still
// meets whatever the forge actually did.
func (e *StepError) Unwrap() []error { return []error{ErrStepPartial, e.Err} }
