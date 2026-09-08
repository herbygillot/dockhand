package publish

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/change"
	"github.com/herbygillot/dockhand/internal/gh"
	"github.com/herbygillot/dockhand/internal/record"
)

// kinds is the advisory kinds of one authorization, so a test can say
// what a person was told without pinning the sentences.
func kinds(adv []Advisory) []string {
	out := make([]string, 0, len(adv))
	for _, a := range adv {
		out = append(out, a.Kind)
	}
	return out
}

// A CACHED STANDING NEVER REACHES A DECISION. It is the first thing
// asked and not the sixth rung it is listed as, because three rungs
// below it read the forge: a ladder that weighed the duplicate, the
// merged dead end and the no-op over what a previous pass wrote down
// would have decided before it noticed it was deciding.
func TestAuthorizeRefusesFactsTheForgeWasNotAskedFor(t *testing.T) {
	f := facts(func(f *Facts) { f.Forge.Fresh = false })
	// Everything else about this fact set passes, and the hold below would
	// otherwise refuse first — so the refusal proves the ORDER and not
	// merely that the field is read.
	f.Change.Hold = &record.Hold{Origin: record.HoldPerson, Reason: "wait"}
	_, adv, err := Authorize(f, Pace{})
	require.ErrorIs(t, err, ErrNotFresh)
	assert.Empty(t, adv)
}

// A PERSON'S HOLD REFUSES BOTH ROADS; A CROSSING'S REFUSES THE MACHINE
// AND ADVISES THE PERSON. The table is change.Held's and not this
// function's, which is what stops the two roads getting different
// answers about one branch.
func TestTheHoldIsTheFirstRungAndAnswersByOrigin(t *testing.T) {
	person := func(f *Facts) {
		f.Change.Hold = &record.Hold{Origin: record.HoldPerson, Reason: "not yet"}
	}
	crossing := func(f *Facts) {
		f.Change.Crossing = record.StableToPrerelease
		f.Change.Hold = &record.Hold{Origin: record.HoldCrossing, Reason: "leaves stable"}
	}

	_, _, err := Authorize(facts(person), Pace{})
	require.ErrorIs(t, err, change.ErrHeld)
	_, _, err = Authorize(facts(machine, person), DefaultPace)
	require.ErrorIs(t, err, change.ErrHeld)

	_, _, err = Authorize(facts(machine, crossing), DefaultPace)
	require.ErrorIs(t, err, change.ErrHeld)

	// The person is WARNED and allowed through: a maintainer asking for a
	// release candidate by name is asking for a legitimate thing, and
	// dockhand does not second-guess a typed version.
	p, adv, err := Authorize(facts(crossing), Pace{})
	require.NoError(t, err)
	assert.Contains(t, kinds(adv), AdviseCrossing)
	assert.Equal(t, []record.StepKind{record.PushBranch, record.OpenPR}, p.Steps())
}

// THE EVIDENCE GATE REFUSES ONLY NEGATIVE EVIDENCE, and negative means
// FAILED. The sketch's parenthesis says "a Failed, Blocked or
// Unsupported attempt"; the lines it cites refuse on Failed alone,
// record's own words call Unsupported "often the change working, so it
// is not a failure" and Blocked "untested, not disproven", and the
// design resolves the same question twice with "refuses only negative
// evidence, as the shipped road does". So both are advisories.
func TestBlockedAndUnsupportedAdviseAndDoNotRefuse(t *testing.T) {
	f := facts(func(f *Facts) {
		f.Change.Subjects = append(f.Change.Subjects,
			record.Subject{Port: "libfoo", Portdir: "devel/libfoo"})
		f.Attempts[0].Runs = map[string]record.Run{
			"jq":     {State: record.Blocked, Detail: "libwidget failed", Blamed: "libwidget"},
			"libfoo": {State: record.Unsupported},
		}
	})
	p, adv, err := Authorize(f, Pace{})
	require.NoError(t, err)
	assert.True(t, p.granted)
	assert.Contains(t, kinds(adv), AdviseUnverified)
	assert.Contains(t, kinds(adv), AdviseBlocked)
}

// A COMPLETED FAILURE IS THE ONE REFUSAL ON THE HUMAN ROAD, and
// --ignore is the deliberate override — which turns it into an advisory
// the body states rather than skipping anything. Which is why the flag
// is Ignore and not NoVerify: it says "ignore the verdict", not "skip
// the work".
func TestAFailureRefusesAPersonUntilIgnore(t *testing.T) {
	failed := func(f *Facts) {
		f.Attempts[0].Runs = map[string]record.Run{"jq": {State: record.Failed}}
	}
	_, _, err := Authorize(facts(failed), Pace{})
	require.ErrorIs(t, err, ErrFailed)

	var typed *FailedError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, []string{"Sequoia"}, typed.Platforms)

	p, adv, err := Authorize(facts(failed, func(f *Facts) { f.Asks.Ignore = true }), Pace{})
	require.NoError(t, err)
	assert.True(t, p.granted)
	assert.Contains(t, kinds(adv), AdviseIgnored)
}

// --ignore IS A PERSON'S JUDGMENT WRITTEN DOWN, so the machine road does
// not honour it. It is not refused as a flag — a caller that set it by
// accident gets the reading that cannot be argued with — it is simply
// not read.
func TestTheMachineNeverHonoursIgnore(t *testing.T) {
	_, _, err := Authorize(facts(machine, func(f *Facts) {
		f.Asks.Ignore = true
		f.Attempts[0].Runs = map[string]record.Run{"jq": {State: record.Failed}}
	}), DefaultPace)
	require.ErrorIs(t, err, ErrFailed)
}

// THE MACHINE REQUIRES POSITIVE EVIDENCE. A person publishes an
// unverified branch with a complaint — they are looking at the complaint
// — and an unattended pass has nobody looking, so absence of evidence
// there is the absence of any reason to spend a reviewer's attention at
// all.
func TestTheMachineRefusesAnUnprovenTipThatAPersonMayPublish(t *testing.T) {
	bare := func(f *Facts) { f.Attempts = nil }

	p, adv, err := Authorize(facts(bare), Pace{})
	require.NoError(t, err)
	assert.True(t, p.granted)
	assert.Equal(t, []string{AdviseUnverified}, kinds(adv))

	_, _, err = Authorize(facts(machine, bare), DefaultPace)
	require.ErrorIs(t, err, ErrUnproven)
}

// A RUN STILL GOING IS PENDING AND NOT A REFUSAL: nothing is wrong,
// nothing is settled, and the next pass asks again. A caller that read
// it as a refusal would page somebody about work proceeding exactly as
// it should.
func TestAnUnfinishedRunIsPendingForTheMachine(t *testing.T) {
	_, _, err := Authorize(facts(machine, func(f *Facts) {
		f.Attempts[0].Phase = record.Active
		f.Attempts[0].Lease = "lease-1"
		f.Attempts[0].Runs = map[string]record.Run{"jq": {State: record.Running}}
	}), DefaultPace)
	require.ErrorIs(t, err, ErrPending)

	var typed *PendingError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, []string{"Sequoia"}, typed.Platforms)
}

// A MACHINE THIS BUILD GRANTS NOTHING IS TOLD SO, and it is told before
// its evidence is weighed: a machine that may publish nothing has
// nothing to weigh evidence for, and Grant names the permission rather
// than the withholding precisely so that reading the field answers the
// question.
func TestGrantNothingRefusesBeforeTheEvidenceIsRead(t *testing.T) {
	_, _, err := Authorize(facts(machine, func(f *Facts) {
		f.Unattended = GrantNothing
		f.Attempts[0].Runs = map[string]record.Run{"jq": {State: record.Failed}}
	}), DefaultPace)
	require.ErrorIs(t, err, ErrNoGrant)
}

// AN UNANSWERED FINDING REFUSES THE MACHINE AND ADVISES THE PERSON.
// There is nobody on the unattended road to have read the question.
func TestAnOpenProposalRefusesOnlyTheMachine(t *testing.T) {
	proposed := func(f *Facts) {
		f.Change.Findings = []record.Finding{{
			Kind: record.KindABIDependents, Disposition: record.Proposed,
			Criterion: "install name libjq.1.dylib -> libjq.2.dylib",
		}}
	}
	_, _, err := Authorize(facts(machine, proposed), DefaultPace)
	require.ErrorIs(t, err, ErrProposalOpen)

	_, adv, err := Authorize(facts(proposed), Pace{})
	require.NoError(t, err)
	assert.Contains(t, kinds(adv), AdviseProposal)
}

// AN UNANSWERED FORGE QUESTION IS A REFUSAL FOR A MACHINE, and it is
// asked BEFORE the duplicate and the merged rungs: an own-PR lookup that
// failed reads as "this branch has no pull request", which is how an
// unattended pass comes to open a second one beside somebody's first.
func TestASilentForgeRefusesTheMachineAndAdvisesThePerson(t *testing.T) {
	silent := func(f *Facts) { f.Forge.Err = assertErr }

	_, _, err := Authorize(facts(machine, silent), DefaultPace)
	require.ErrorIs(t, err, ErrForgeSilent)
	require.ErrorIs(t, err, assertErr)

	_, adv, err := Authorize(facts(silent), Pace{})
	require.NoError(t, err)
	assert.Contains(t, kinds(adv), AdviseForge)
}

// A DUPLICATE REFUSES WITH THE OTHER PULL REQUEST IN HAND, so a caller
// can offer it rather than describe it — and --no-pr-check publishes
// past it deliberately, for a person and never for a machine.
func TestADuplicateRefusesAndNoPRCheckIsAPersonsOverride(t *testing.T) {
	dup := func(f *Facts) {
		f.Forge.DuplicateFound = true
		f.Forge.Duplicate = gh.PullRequest{Number: 42, Title: "jq: update to 1.8",
			State: "open", HTMLURL: "https://example.invalid/pull/42"}
	}
	_, _, err := Authorize(facts(dup), Pace{})
	require.ErrorIs(t, err, ErrDuplicate)
	var typed *DuplicateError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, 42, typed.Number)

	_, _, err = Authorize(facts(dup, func(f *Facts) { f.Asks.NoPRCheck = true }), Pace{})
	require.NoError(t, err)

	_, _, err = Authorize(facts(machine, dup, func(f *Facts) { f.Asks.NoPRCheck = true }), DefaultPace)
	require.ErrorIs(t, err, ErrDuplicate)
}

// THE SAME-PORT PULL REQUESTS ARE ADVISORIES AND NOT A REFUSAL, and they
// are VALUES on the result rather than lines the search printed as it
// walked — which is the shipped defect this removes: no caller could
// count them, hold them, or render them anywhere but a terminal.
func TestSamePortPullRequestsAreAdvisories(t *testing.T) {
	_, adv, err := Authorize(facts(func(f *Facts) {
		f.Forge.SamePort = []gh.PullRequest{{
			Number: 7, Title: "jq: update to 1.9", State: "open",
			HTMLURL: "https://example.invalid/pull/7",
			Head:    gh.PRHead{Ref: "dockhand/jq-1.9"},
		}}
	}), Pace{})
	require.NoError(t, err)
	require.Contains(t, kinds(adv), AdviseSamePort)
	for _, a := range adv {
		if a.Kind == AdviseSamePort {
			// The version is read off the branch NAME, which is a construction
			// dockhand made and can therefore invert, and the note says where
			// it was read from so a reader can weigh the source.
			assert.Contains(t, a.Text, "1.9")
			assert.Contains(t, a.Text, "newer than this publication's 1.8")
			assert.Contains(t, a.Text, "version read from its branch name dockhand/jq-1.9")
		}
	}
}

// A MERGED PULL REQUEST IS A DEAD END ON BOTH ROADS: there is nothing
// left to publish, and pushing to that branch would resurrect work the
// project has already taken.
func TestAMergedPullRequestRefusesBothRoads(t *testing.T) {
	merged := func(f *Facts) {
		f.Forge.OwnFound = true
		f.Forge.Own = gh.PullRequest{Number: 3, State: "closed", MergedAt: "2026-09-01T00:00:00Z",
			HTMLURL: "https://example.invalid/pull/3", Head: gh.PRHead{Ref: "dockhand/jq-1.8", Sha: "aaaa"}}
	}
	for _, road := range []func(*Facts){func(*Facts) {}, machine} {
		_, _, err := Authorize(facts(merged, road), DefaultPace)
		require.ErrorIs(t, err, ErrMerged)
	}
}

// AN OPEN OWN PULL REQUEST AT THE SAME TIP IS A NO-OP PERMIT: no steps,
// nothing performed, nothing recorded, and never a spend. Without it a
// resident dispatcher at five minutes re-applies publication to every
// open pull request — 288 forge writes per PR per day, each counted
// against the allowance.
func TestAnOpenPullRequestAtThisTipIsANoOp(t *testing.T) {
	p, _, err := Authorize(facts(func(f *Facts) {
		f.Forge.OwnFound, f.Forge.Own = true, openPR(9, "aaaa")
	}), Pace{})
	require.NoError(t, err)
	assert.True(t, p.NoOp())
	assert.Empty(t, p.Steps())

	// Apply on one performs and records nothing, and reaches no
	// repository, no forge and no store to do it.
	out, err := Apply(t.Context(), Env{}, p)
	require.NoError(t, err)
	assert.Empty(t, out.Completed)
	assert.Equal(t, 9, out.Number)
}

// A BRANCH THAT MOVED UNDER AN OPEN PULL REQUEST IS A REFRESH, AND A
// REFRESH IS REACHABLE BY A MACHINE. The shipped tree refused an
// unattended republication outright because it could not tell a
// dispatcher re-applying publication on every tick from one moving a
// pull request it opened itself; the no-op above is that difference, and
// Spent never counts a refresh.
func TestAMovedBranchRefreshesItsOwnPullRequest(t *testing.T) {
	moved := func(f *Facts) {
		f.Forge.OwnFound, f.Forge.Own = true, openPR(9, "bbbb")
	}
	p, _, err := Authorize(facts(moved), Pace{})
	require.NoError(t, err)
	assert.False(t, p.NoOp())
	assert.Equal(t, []record.StepKind{record.PushBranch, record.RefreshPR}, p.Steps())

	p, _, err = Authorize(facts(machine, moved), DefaultPace)
	require.NoError(t, err)
	assert.Equal(t, []record.StepKind{record.PushBranch, record.RefreshPR}, p.Steps())
}

// --no-pr STOPS AT THE PUSH, which is a publication too: the branch goes
// to the person's own fork, which is theirs and deletable at will, and
// spends nobody's attention.
func TestNoPRStopsAtThePush(t *testing.T) {
	p, _, err := Authorize(facts(func(f *Facts) { f.Asks.NoPR = true }), Pace{})
	require.NoError(t, err)
	assert.Equal(t, []record.StepKind{record.PushBranch}, p.Steps())
}

// THE MACHINE PUBLISHES A SIMPLE BUMP AND NOTHING ELSE, and Unjudged
// withholds by rule 7 — "I could not classify it" is not "it is
// confined".
func TestTheMachinePublishesOnlyAConfinedChange(t *testing.T) {
	for _, verdict := range []change.Simplicity{change.NotSimple, change.Unjudged} {
		_, _, err := Authorize(facts(machine, func(f *Facts) {
			f.Simplicity = verdict
			f.Why = []string{"it edits the toolchain minimum"}
		}), DefaultPace)
		require.ErrorIs(t, err, ErrNotSimple)
		var typed *NotSimpleError
		require.ErrorAs(t, err, &typed)
		assert.Equal(t, []string{"it edits the toolchain minimum"}, typed.Why)
	}
	// A person is never asked: the confinement is the machine's condition
	// and a person may publish anything they could type.
	_, _, err := Authorize(facts(func(f *Facts) { f.Simplicity = change.NotSimple }), Pace{})
	require.NoError(t, err)
}

// THE DOWNGRADE REFUSAL HANGS ON THE MOVEMENT AND NOT ON THE EDIT KINDS,
// which is change.Judge's own warning: a downgrade whose epoch edit the
// planner could not emit arrives at the judge as {Version, Checksum,
// VendoredBlock} — every one of them permitted — so Simple is not
// enough.
func TestAMachineRefusesADowngradeThatJudgeCalledSimple(t *testing.T) {
	_, _, err := Authorize(facts(machine, func(f *Facts) {
		f.Direction.Movement.Upgrades = false
		f.Direction.EpochOwed = true
		f.Direction.Movement.From.Version = "1.8"
		f.Direction.Movement.To.Version = "1.7"
	}), DefaultPace)
	require.ErrorIs(t, err, ErrEpochOwed)

	// The person emits the epoch edit and is told; nothing refuses.
	_, adv, err := Authorize(facts(func(f *Facts) { f.Direction.EpochOwed = true }), Pace{})
	require.NoError(t, err)
	assert.Contains(t, kinds(adv), AdviseEpoch)
}

// A MOVEMENT THAT COULD NOT BE COMPARED WITHHOLDS (rule 7). "I could not
// find out" is not "it moves forward".
func TestAnUncomparedMovementRefusesTheMachine(t *testing.T) {
	_, _, err := Authorize(facts(machine, func(f *Facts) { f.Direction = Direction{} }), DefaultPace)
	require.ErrorIs(t, err, ErrDirectionUnknown)
}

// THE PORTDIR MOVING UNDERNEATH A CHANGE REFUSES THE MACHINE — somebody
// else has touched this port upstream, which is a judgment about whose
// change should land — and a drift that could not be MEASURED refuses it
// too, because BehindBy zero would otherwise say "up to date".
//
// Being merely behind is not a refusal on either road: every branch is
// behind a moving main within the hour.
func TestDriftRefusesTheMachineAndAdvisesThePerson(t *testing.T) {
	over := func(f *Facts) { f.Drift = change.Drift{Compared: true, OverTree: true, BehindBy: 4} }
	unknown := func(f *Facts) { f.Drift = change.Drift{Err: change.ErrNoBase} }

	for _, d := range []func(*Facts){over, unknown} {
		_, _, err := Authorize(facts(machine, d), DefaultPace)
		require.ErrorIs(t, err, ErrDrifted)
		_, adv, err := Authorize(facts(d), Pace{})
		require.NoError(t, err)
		assert.Contains(t, kinds(adv), AdviseDrift)
	}

	// Behind, and nothing more: an advisory on both roads and a refusal on
	// neither.
	behind := func(f *Facts) { f.Drift = change.Drift{Compared: true, BehindBy: 12} }
	_, adv, err := Authorize(facts(machine, behind), DefaultPace)
	require.NoError(t, err)
	assert.Contains(t, kinds(adv), AdviseDrift)
}

// A BODY THE FORGE WILL NOT TAKE IS REFUSED BEFORE ANYTHING LEAVES THE
// MACHINE, and it is not trimmed to fit: the member lines are the
// evidence a reviewer is asked to accept, so a body that omitted some of
// them would be vouching for what it does not show.
func TestABodyPastTheForgesLimitIsRefusedAndNotTrimmed(t *testing.T) {
	long := make([]byte, gh.MaxPRBody+1)
	for i := range long {
		long[i] = 'x'
	}
	_, _, err := Authorize(facts(func(f *Facts) { f.Body = string(long) }), Pace{})
	require.ErrorIs(t, err, ErrBodyTooLong)
	var typed *BodyTooLongError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, gh.MaxPRBody+1, typed.Size)
	assert.Equal(t, gh.MaxPRBody, typed.Limit)
}

// A COHORT MEMBER PUBLISHED WITHOUT A PASS IS SAID OUT LOUD. The
// dependents are best effort, so this is the ordinary shape rather than
// an error — and the number is what record.PublicationState.Unproven
// carries.
func TestAnUnprovenMemberIsAnAdvisory(t *testing.T) {
	_, adv, err := Authorize(facts(func(f *Facts) {
		f.Change.Subjects = append(f.Change.Subjects,
			record.Subject{Port: "libfoo", Portdir: "devel/libfoo"})
		f.Attempts[0].Runs["libfoo"] = record.Run{State: record.Failed}
	}), Pace{})
	require.NoError(t, err)
	assert.Contains(t, kinds(adv), AdviseUnproven)
}

// A VERIFICATION STILL IN FLIGHT ON THE TIP IS AN ADVISORY THE PERMIT
// CARRIES, never a cancellation. Publication does not stop a build and
// has no way to: cancelling is a thing a person asks for, with its own
// verb.
func TestARunningVerificationOnTheTipIsAnAdvisoryAndNotACancel(t *testing.T) {
	p, _, err := Authorize(facts(func(f *Facts) {
		f.Attempts = append(f.Attempts, record.Attempt{
			ID: "att-2", Change: "chg-1", Sha: "aaaa", Content: "tree-1",
			Platform: "Sonoma", Phase: record.Active, Lease: "lease-2",
			Started: clock.Add(-time.Minute),
		})
	}), Pace{})
	require.NoError(t, err)
	assert.Equal(t, []string{"Sonoma"}, p.Running())
	// The verdict set is untouched: the permit was granted and nothing
	// about the running attempt was written.
	assert.Equal(t, []record.StepKind{record.PushBranch, record.OpenPR}, p.Steps())
}

// assertErr is the forge's silence in the tests above: an identity, so a
// test can prove the cause survives the wrapping without pinning
// anybody's prose.
var assertErr = errForTest("the forge said no")

type errForTest string

func (e errForTest) Error() string { return string(e) }

// AN INVOCATION THAT NEVER SAID WHO IT WAS IS A WIRING GAP AND NOT A
// PERSON (rule 7). The zero Driver would be read as the permissive road
// — --ignore honoured, no grant asked, no pace counted, no confinement
// weighed — and every one of those gates exists for the unattended one.
func TestAuthorizeRefusesFactsThatDoNotSayWhoIsAsking(t *testing.T) {
	_, _, err := Authorize(facts(func(f *Facts) { f.Invoker = "" }), Pace{})
	require.ErrorIs(t, err, ErrNoInvoker)

	// And the three asks a person may spend are not honoured for it
	// either, so a gap that reached further would still withhold.
	var none record.Driver
	assert.False(t, Asks{Ignore: true, NoPRCheck: true, Force: true}.ignore(none))
	assert.False(t, Asks{Ignore: true, NoPRCheck: true, Force: true}.noPRCheck(none))
	assert.False(t, Asks{Ignore: true, NoPRCheck: true, Force: true}.force(none))
}

// AN INSTRUCTION COMMENT DOES NOT BLOCK A PERSON. ONLY THE MACHINE.
// Ruled 8 September 2026, and the gate above is what implements it — but
// this kind could never reach the gate: record.FindingKind spelled
// "instruction" while every planner wrote "instruction-comment", so
// change.stamp refused the word and MintIn failed the whole Amend. The
// port never got a branch, let alone an advisory.
//
// Pinned by kind rather than left to the generic proposal test, because
// the ruling is about this finding: a maintainer's own written
// instruction is evidence no measurement produces, it can be wrong, and
// what dockhand does with it is quote it and let a person decide.
func TestAnInstructionCommentAdvisesThePersonAndRefusesTheMachine(t *testing.T) {
	instructed := func(f *Facts) {
		f.Change.Findings = []record.Finding{{
			Kind: record.KindInstruction, Disposition: record.Proposed,
			Source: "sysutils/jq/Portfile", Quote: "revbump dependents when this moves",
		}}
	}
	_, _, err := Authorize(facts(machine, instructed), DefaultPace)
	require.ErrorIs(t, err, ErrProposalOpen,
		"there is nobody on the unattended road to have read the comment")

	_, adv, err := Authorize(facts(instructed), Pace{})
	require.NoError(t, err, "a person publishing past their own advisory is their answer")
	assert.Contains(t, kinds(adv), AdviseProposal)
}

// AND A PATCH DOCKHAND COULD NOT CHECK BLOCKS NEITHER. It is a
// statement and not a question — Accepted, not Proposed — so it says
// what was not checked and holds nothing waiting for an answer nobody
// can give.
func TestAnUncheckedPatchFindingHoldsNeitherRoad(t *testing.T) {
	unchecked := func(f *Facts) {
		f.Change.Findings = []record.Finding{{
			Kind: record.KindPatchesUnchecked, Disposition: record.Accepted,
			Criterion: "patch check unavailable: jq's 2 patchfiles were not checked against the new source",
		}}
	}
	_, adv, err := Authorize(facts(machine, unchecked), DefaultPace)
	require.NoError(t, err)
	assert.NotContains(t, kinds(adv), AdviseProposal)

	_, _, err = Authorize(facts(unchecked), Pace{})
	require.NoError(t, err)
}

// A PATCH THE BUMP COULD NOT CARRY OVER IS A QUESTION FOR A PERSON, and
// the person is exactly who can answer it: they judge what the patch was
// for, resolve it on the branch, and verify or promote. There is nobody
// on the unattended road to do that.
//
// It used to decline the whole bump, so this branch never existed. Ruled
// 8 September 2026.
func TestAnUnrelocatedPatchAdvisesThePersonAndRefusesTheMachine(t *testing.T) {
	stuck := func(f *Facts) {
		f.Change.Findings = []record.Finding{{
			Kind: record.KindPatchUnrelocated, Disposition: record.Proposed,
			Source:    "files/patch-foo.diff",
			Criterion: "patch not carried over: files/patch-foo.diff does not relocate onto the new source — Makefile hunk #1: its before-block occurs nowhere in the file. Refresh it by hand on the branch, then verify",
		}}
	}
	_, _, err := Authorize(facts(machine, stuck), DefaultPace)
	require.ErrorIs(t, err, ErrProposalOpen)

	_, adv, err := Authorize(facts(stuck), Pace{})
	require.NoError(t, err, "a person may publish past their own advisory; that is their answer")
	assert.Contains(t, kinds(adv), AdviseProposal)
}
