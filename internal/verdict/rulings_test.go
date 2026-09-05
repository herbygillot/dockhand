package verdict

// The maintainer's rulings, one test per ruling, named for it.
//
// This file is the INDEX. Every ruling the maintainer made about who may
// publish, what stops a run, and what a branch's end states mean has a
// test here that states it in its own words, so a reader who wants to
// know what was decided reads nine test names rather than nine
// packages, and a ruling reversed by accident fails in one place.
//
// TWO KINDS OF EVIDENCE, because the index sits in verdict and verdict
// may not import the layers that act.
//
// Where the judgment is verdict's own, the ruling is EXERCISED: the
// function is called and its answer asserted, which is the strongest
// thing a test can do.
//
// Where the behaviour lives above verdict — in the engine that pushes
// and cancels, in the cmd that resolves the invoker — the depguard rule
// on this package's tests (see .golangci.yml, verdict-tests) forbids the
// import, and rightly: a judgment that could reach the engine would stop
// being a judgment. So those rulings are pinned two ways instead. The
// SOURCE is asserted, over the lines the compiler sees, for the
// structural fact that makes the behaviour hard to lose — the cancel is
// inside the human guard, the permission constant is false, no invoker
// path reads a terminal. And the behavioural test that actually runs the
// code is required to still EXIST, by name, in the package that can run
// it. Deleting the guard fails here; deleting its test fails here too.
//
// What this file must never become is a second copy of those tests.
// Where a ruling names a test elsewhere, that test is the proof and this
// one is the index entry; the comment on each says which is which.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/herbygillot/dockhand/internal/exitcode"
	"github.com/herbygillot/dockhand/internal/record"
	"github.com/herbygillot/dockhand/internal/verify"
)

// THE RULING: a person promoting mid-verification cancels the running
// build, and is told that it happened.
//
// Typing promote while a verification runs IS the answer about that
// build — the tool removes friction rather than making somebody wait for
// evidence they have already decided not to read. What the ruling
// insists on is the second half: the cancellation is not silent. A run
// that vanished without a line would leave the promoter believing the PR
// carries a verdict it does not.
//
// Exercised end to end by TestPromoteMidVerificationCancelsAndProceeds
// in internal/cmd. Pinned here as the shape that makes it structural:
// the only cancel in Promote is inside the human guard, and it prints.
func TestAHumanPromoteCancelsAndIsTold(t *testing.T) {
	promote := bodyOf(t, compiledFile(t, "engine/promote.go"), "func (e *Engine) Promote(")
	guarded := bodyOf(t, promote, "if o.invoker() == record.Human {")

	assert.Equal(t, 1, matches(guarded, `e\.cancelRuns\(`),
		"a person's promote cancels the run it is not waiting for")
	assert.Equal(t, 1, matches(guarded, `"canceled: promoted without waiting"`),
		"and the note says why the run stopped, so the reason outlives the terminal")
	assert.Equal(t, 1, matches(guarded, `Fprintf\(e\.Err, "canceled %d running verification`),
		"and the person is told; a silent cancel leaves them believing in a verdict that is not there")

	assert.Equal(t, 1, matches(promote, `cancelRuns\(`),
		"the guarded cancel is the only one Promote has")

	heldBy(t, "cmd/promote_lifecycle_test.go", "TestPromoteMidVerificationCancelsAndProceeds")
}

// THE RULING: a machine never cancels. The one cancellation it may cause
// is a supersede.
//
// Every argument for the ruling above is about a person's intent —
// promoting mid-verification says something about the running build —
// and an unattended pass has no intent to read. Left ungated, one pass
// would kill every verification in progress on the machine and then
// publish the unverified result it had just created.
//
// The exception is not an exception to the reasoning. SupersedeStale
// stops runs whose branch moved out from under them: it is about the
// commit having been replaced, not about anybody's patience, and the
// answer those runs were going to give is about bytes that are no longer
// the tip.
//
// Exercised by TestAMachineNeverCancelsEvenWhereItMayPublish in
// internal/engine — which grants the permission first, so the property
// is proven to be the guard and not an accident of the constant being
// false. Pinned here as the census: where a run may be stopped at all.
func TestAMachineNeverCancelsExceptBySupersede(t *testing.T) {
	assert.Equal(t, map[string]int{
		// The `cancel` verb: a person, naming a branch.
		"internal/engine/cancel.go": 1,
		// Promote's mid-verification cancel, inside the human guard the
		// ruling above pins.
		"internal/engine/promote.go": 1,
	}, spelledIn(t, `\.cancelRuns\(`),
		"a run is stopped in two places, and the unattended road is neither of them")

	// The machine road's own files, named so that a cancel appearing in
	// one of them fails by naming the file rather than by a count that
	// moved. These are the files an unattended pass executes.
	for _, road := range []string{"engine/publishslot.go", "engine/rewitness.go", "engine/reconcile.go"} {
		assert.Equal(t, 0, matches(compiledFile(t, road), `cancelRuns\(`),
			"%s runs unattended and must stop nothing", road)
	}

	// The exception, and the whole of it: the supersede sweep, which
	// marks a superseded run rather than a canceled one — the state says
	// the branch moved, not that somebody lost patience.
	assert.Equal(t, map[string]int{"internal/engine/cancel.go": 2}, spelledIn(t, `= record\.Superseded`),
		"a run is superseded in one place, and it is the sweep that watches the branch move")
	stale := bodyOf(t, compiledFile(t, "engine/cancel.go"), "func (e *Engine) SupersedeStale(")
	assert.Equal(t, 2, matches(stale, `= record\.Superseded`),
		"and that place is SupersedeStale: a run that was going and one that had failed")

	heldBy(t, "engine/machinegate_test.go", "TestAMachineNeverCancelsEvenWhereItMayPublish")
}

// THE RULING: a rejected change is never retired.
//
// A pull request closed without merging is information — somebody looked
// at this change and said no — and the branch that holds the diff, the
// evidence and the reason is the only copy of it. Deleting that on the
// grounds that the pull request is finished would destroy the answer and
// keep the question.
//
// This one is verdict's own, so it is exercised rather than pinned:
// merged is the single verdict that deletes, and rejection is a kept end
// state with wording that says why it is kept.
func TestRejectedIsNeverRetired(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verdict  Retirement
		deletes  bool
		contains string
	}{
		{"never promoted", RetireUnpromoted, false, "kept"},
		{"promoted with no PR", RetireNoPR, false, "kept"},
		{"merged", RetireMerged, true, "cleaned"},
		{"open", RetireOpen, false, "kept"},
		{"closed without merging", RetireClosed, false, "rejection is information"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.deletes, tc.verdict.Cleans(false),
				"the merged verdict is the only one that deletes")
			assert.False(t, tc.verdict.Cleans(true),
				"--no-clean withholds every deletion without changing any verdict")
			assert.Contains(t, tc.verdict.SweepLine(closed, true), tc.contains)
		})
	}

	// And the judgment that produces it: a pull request that closed
	// without a merge is RetireClosed, whatever else is true of it.
	require.Equal(t, RetireClosed, DecideRetire(true, closed))
	assert.False(t, DecideRetire(true, closed).Cleans(false))
	assert.Contains(t, Reconciliation{Promoted: true, PR: closed}.Line(), "closed without merging",
		"status reports the rejection and acts on nothing")

	// Rejection reaches exactly one writer in the tree, and it writes an
	// audit row rather than removing anything. A second spelling would be
	// the ruling's only plausible way to break.
	assert.Equal(t, map[string]int{"internal/engine/reconcile.go": 1}, spelledIn(t, `record\.Rejected`),
		"a rejection is recorded once, in the outcome log, and acted on nowhere")
}

// THE RULING: a machine with no local verifier exits ZERO inside a write
// intent.
//
// The implicit submit that follows a bump or a refresh asks for a
// verification the machine may be unable to give. Having no tart at all
// is not a failure of the change: the branch was minted, it stands, and
// it may be promoted as it is. So the contract NARROWS — the verb says
// the branch is unverified and succeeds — rather than failing and
// leaving the user with a minted branch and a non-zero status about it.
//
// The verbs that were ASKED to verify are the other half, and they do
// fail: verify, log, shell and exec meet the same sentinel and exit in
// the machine band, because a missing provider is exactly what they
// could not do.
//
// Exercised by TestSubmitWithoutAProviderNarrowsTheContract in
// internal/engine, with the explicit half held in cmd's exit table.
func TestNoLocalVerifierExitsZeroInsideAWriteIntent(t *testing.T) {
	narrowed := bodyOf(t, bodyOf(t, compiledFile(t, "engine/submit.go"), "func (e *Engine) submit("),
		"if errors.Is(err, verify.ErrNoProvider) {")
	assert.Equal(t, 1, matches(narrowed, `^\s*return nil$`),
		"no provider, no contract: the branch stands and the write intent succeeds")
	assert.Equal(t, 1, matches(narrowed, `no verification possible`),
		"and it says so, because an unverified branch the user does not know about is the failure mode")
	assert.Equal(t, 1, matches(narrowed, `you may promote it as is`),
		"and says what is still allowed, which is the whole of the narrowing")

	// The asked-for verbs, in the band a missing tool belongs to.
	asked := bodyOf(t, compiledFile(t, "cmd/exit.go"), "func codeAndReason(")
	assert.Equal(t, 1, matches(asked, `errors\.Is\(err, verify\.ErrNoProvider\)`),
		"the explicit verbs map the same sentinel, and they map it to a refusal")
	assert.Equal(t, 1, matches(asked, `exitcode\.ToolMissing, "no-verify-provider"`))
	assert.Equal(t, "environment", exitcode.Family(exitcode.ToolMissing),
		"a tool that is not installed is the machine's problem and has an install remedy")
	assert.Equal(t, 0, exitcode.OK,
		"and the implicit submit's answer is the shell's own success")

	heldBy(t, "engine/deferred_band_test.go", "TestSubmitWithoutAProviderNarrowsTheContract")
}

// THE RULING: the exit bands. A status answers "whose problem is this",
// the decade IS the family, and a caller reading $?/10 keeps working
// when a code it has never heard of is added beside the ones it knows.
//
// The table below is the contract as ruled — every code, its number, and
// the band it belongs to. It is written out longhand rather than derived
// from the constants, because a table that computed its own answers
// would agree with any renumbering, and the numbers are what scripts
// branch on once dockhand ships.
func TestTheBandTable(t *testing.T) {
	table := []struct {
		code   int
		want   int
		family string
	}{
		// The three that predate the bands and keep the shell's meanings.
		{exitcode.OK, 0, "success"},
		{exitcode.Failure, 1, "failure"},
		{exitcode.Usage, 2, "usage"},
		// 10-13, declined: the plan's problem.
		{exitcode.PlanDeclined, 10, "declined"},
		{exitcode.BranchInFlight, 11, "declined"},
		{exitcode.AlreadyCurrent, 12, "declined"},
		{exitcode.Ambiguous, 13, "declined"},
		// 20-24, refused: the destination's problem.
		{exitcode.DuplicatePR, 20, "refused"},
		{exitcode.PRMerged, 21, "refused"},
		{exitcode.Superseded, 22, "refused"},
		{exitcode.Held, 23, "refused"},
		{exitcode.MachineGate, 24, "refused"},
		// 30-36, environment: the machine's problem, every one with an
		// installation or provisioning remedy.
		{exitcode.NoMacPorts, 30, "environment"},
		{exitcode.EvalStartup, 31, "environment"},
		{exitcode.RootRefused, 32, "environment"},
		{exitcode.ToolMissing, 33, "environment"},
		{exitcode.NoVerifyEnv, 34, "environment"},
		{exitcode.ProvisionFailed, 35, "environment"},
		{exitcode.VerifierBusy, 36, "environment"},
		// 40-44, tree: where dockhand was pointed.
		{exitcode.NotPortsTree, 40, "tree"},
		{exitcode.PortNotFound, 41, "tree"},
		{exitcode.NotARepo, 42, "tree"},
		{exitcode.Drift, 43, "tree"},
		{exitcode.BranchNotFound, 44, "tree"},
		// 50-53, upstream: somebody else's.
		{exitcode.FetchFailed, 50, "upstream"},
		{exitcode.WitnessUnreachable, 51, "upstream"},
		{exitcode.WitnessAPI, 52, "upstream"},
		{exitcode.LatestUnresolved, 53, "upstream"},
		// 60-62, pending: nobody's problem yet.
		{exitcode.VerifyQueued, 60, "pending"},
		{exitcode.VerifyAwaitingSlot, 61, "pending"},
		{exitcode.PromotionPending, 62, "pending"},
		// 70-73, verdict: verification answered, and not with a pass.
		{exitcode.VerifyFailed, 70, "verdict"},
		{exitcode.VerifyBlocked, 71, "verdict"},
		{exitcode.VerifyUnsupported, 72, "verdict"},
		{exitcode.VerifyErrored, 73, "verdict"},
		// 80-83, partial: half the work stands.
		{exitcode.MintedSubmitErrored, 80, "partial"},
		{exitcode.PushedPRFailed, 81, "partial"},
		{exitcode.PRRefreshFailed, 82, "partial"},
		{exitcode.SweepHardErrors, 83, "partial"},
	}
	for _, row := range table {
		assert.Equal(t, row.want, row.code, "the numbers are the contract; moving one breaks a script")
		assert.Equal(t, row.family, exitcode.Family(row.code),
			"%d is in the %s band", row.want, row.family)
	}

	// A code outside the contract has no family and says so, rather than
	// guessing at the nearest band. Guessing is how an unknown answer
	// would arrive at a caller wearing a shape it does not have.
	assert.Empty(t, exitcode.Family(99), "an unruled code claims no band")
	assert.Empty(t, exitcode.Family(3), "and neither does a gap below the decades")

	// The twin derives its family rather than spelling one, so the status
	// a document carries and the status the process leaves behind cannot
	// come to disagree.
	twin := exitcode.TwinOf(&UnprovenError{Branch: "dockhand/jq", Tip: "abc1234"})
	assert.Equal(t, exitcode.Twin{Code: exitcode.MachineGate, Family: "refused",
		Reason: "no-positive-evidence"}, twin)
	assert.Equal(t, exitcode.Of(exitcode.OK, ""), exitcode.TwinOf(nil))
}

// THE RULING: a decline that withheld riders exits 12, and one that
// withheld nothing exits 10.
//
// "Nothing to do" and "nothing to do, and here is what that cost" are
// the same outcome with different consequences. A sweep over a hundred
// ports needs to see the second without reading prose written for a
// person: the housekeeping the verb would have carried along — a
// checksum refresh, a revision reset — did not happen, and the port is
// left needing a second invocation somebody has to know to make.
//
// The reason names itself too, rather than sharing the plain
// already-current token: the band says which KIND of problem this is and
// the reason says WHICH, so a consumer filtering on the reason must not
// have to read the code to learn whether anything went undone.
//
// Exercised by TestDeclineWithheldRidersGetTheirOwnCode in internal/plan.
func TestRidersWithheldExitTwelve(t *testing.T) {
	assert.Equal(t, 12, exitcode.AlreadyCurrent)
	assert.Equal(t, "declined", exitcode.Family(exitcode.AlreadyCurrent),
		"a decline is a successful judgment and never the failure band")
	assert.NotEqual(t, exitcode.PlanDeclined, exitcode.AlreadyCurrent,
		"the withheld case is its own code inside the declined band")

	// The code is spent in exactly one place, and only on the pairing the
	// ruling names: already-current AND something held back.
	assert.Equal(t, map[string]int{"internal/plan/decline.go": 1}, spelledIn(t, `exitcode\.AlreadyCurrent`),
		"one writer, so the pairing cannot be spelled a second way")

	band := bodyOf(t, compiledFile(t, "plan/decline.go"), "func (d *Decline) DockhandExit() int {")
	assert.Equal(t, 1, matches(band, `d\.Type == AlreadyCurrent && len\(d\.Withheld\) > 0`),
		"both halves: the port is where it was asked to be, and riders were held back with it")
	assert.Equal(t, 1, matches(band, `return exitcode\.AlreadyCurrent`))
	assert.Equal(t, 1, matches(band, `return exitcode\.PlanDeclined`),
		"a decline that withheld nothing is the ordinary one")

	reason := bodyOf(t, compiledFile(t, "plan/decline.go"), "func (d *Decline) Code() string {")
	assert.Equal(t, 1, matches(reason, `d\.Type == AlreadyCurrent && len\(d\.Withheld\) > 0`),
		"the reason is decided on the same pairing as the band, so the two cannot drift")
	assert.Equal(t, 1, matches(reason, `"already-current-withheld"`))

	heldBy(t, "plan/decline_test.go", "TestDeclineWithheldRidersGetTheirOwnCode")
}

// THE RULING: a supersede is recorded only on a newer target of the same
// port, and only at mint.
//
// Port-keyed, because that is the relation the record's field names:
// dockhand/jq-1.8.2 and dockhand/jq-1.8.3 are one port under two branch
// names, and the in-flight refusal compares branch names, so both stand.
// Something has to say which is the change now.
//
// At mint and never at submit, because a submit happens to whichever
// branch was pointed at — the drain retries an old one, `dockhand verify`
// names one by hand — so writing the field from there would let an older
// branch declare a newer one superseded. A mint is the one moment where
// which of two branches is the newer is not a guess.
//
// And it is recorded and nothing else: `clean --superseded` is the
// intentional removal, and nothing else in the tree touches a superseded
// branch. That half is held by internal/engine/superseded_test.go.
func TestSupersedeOnlyOnANewerTargetOfTheSamePort(t *testing.T) {
	// Two writers of the field, and only one of them makes a claim: the
	// mint. The other carries an existing claim across a commit, because
	// a change a newer sibling replaced is still replaced when it grows.
	assert.Equal(t, map[string]int{
		"internal/engine/mint.go":   1,
		"internal/engine/extend.go": 1,
	}, spelledIn(t, `\.SupersededBy = `),
		"a supersede is claimed in one place and carried in one place")
	assert.Equal(t, 1, matches(compiledFile(t, "engine/extend.go"), `r\.SupersededBy = old\.SupersededBy`),
		"the carry copies what was already claimed; it never claims")

	// The claim itself, asked once and from the mint.
	assert.Equal(t, map[string]int{"internal/engine/mint.go": 1}, spelledIn(t, `e\.supersedeSiblings\(`),
		"a submit that could supersede would let an older branch replace a newer one")

	siblings := bodyOf(t, compiledFile(t, "engine/mint.go"), "func (e *Engine) supersedeSiblings(")
	assert.Equal(t, 1, matches(siblings, `if br == minted \{`),
		"the branch just minted does not supersede itself")
	assert.Equal(t, 2, matches(siblings, `Headline\(\)\.Port != port`),
		"the port is compared on the read and again under the lock, so a note that moved between them is not miskeyed")
	assert.Equal(t, 1, matches(siblings, `r\.SupersededBy = minted`),
		"and what the older branch gains is the name of the newer one")

	heldBy(t, "engine/superseded_test.go", "TestCleanSupersededRemovesTheReplacedBranchAndKeepsTheNewer")
	heldBy(t, "engine/superseded_test.go", "TestTheOrdinarySweepNeverRemovesASupersededBranch")
}

// THE RULING: auto mode is DECLARED and never detected.
//
// A person is the invoker of every verb unless the invocation says
// otherwise, and there are exactly three ways to say otherwise: the
// `auto` verb, the persistent --auto flag, and DOCKHAND_AUTO. Nothing
// asks whether a terminal is attached. tool.IsTerminal exists and is one
// import away, and reaching for it would make the answer depend on how
// the process was started rather than on what the operator said: a pipe,
// a CI runner or a `script` wrapper would each silently move a human's
// authority onto a machine, or the reverse, with nothing in the
// invocation for anybody to read.
//
// Exercised by TestAutoModeIsDeclaredAndNeverDetected in internal/cmd,
// which drives the resolution through the real command tree. Pinned here
// as the prohibition: the invoker layer contains no detection at all,
// and the tree's one terminal read is somewhere else entirely.
func TestAutoModeIsDeclaredAndNeverDetected(t *testing.T) {
	// The layers that decide who is running, or that carry the answer:
	// the command line, the run state it resolves into, the entrypoint
	// above both, AND the engine — which is where the declaration becomes
	// the record's provenance (Policy.Invoker, Policy.askedBy) and where
	// the machine road hard-codes its own driver. Leaving the engine out
	// was the gap: a detection written into Policy.askedBy would decide
	// every minted record's asked_by and no walk in the tree would have
	// said so.
	//
	// Comment lines are already dropped, so the prohibition may be NAMED
	// in prose — auto.go's own doc comment names it, and so does this one
	// — while no line that compiles may spell it.
	detection := regexp.MustCompile(`IsTerminal|isatty|ModeCharDevice|os\.Std(in|out|err)\.Stat\(`)
	// The one legitimate hit inside those directories, allowed by name
	// and by count. engine/run.go's diffFromPlan asks whether stdout is a
	// terminal to decide whether to PAGE A DIFF, which is a rendering
	// question with no bearing on who asked for anything. Naming it here
	// rather than excusing the file is what makes a second read in the
	// same file fail.
	allowed := map[string]int{filepath.Join(internalDir, "engine", "run.go"): 1}
	for _, dir := range []string{filepath.Join(internalDir, "cmd"), filepath.Join(internalDir, "runstate"),
		filepath.Join(internalDir, "engine"), mainDir} {
		require.NoError(t, filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			found := 0
			for i, line := range compiledOnly(readFile(t, path)) {
				if !detection.MatchString(line) {
					continue
				}
				found++
				assert.LessOrEqual(t, found, allowed[path],
					"%s: line %d of the compiled source detects a terminal; auto mode is declared", path, i+1)
			}
			assert.Equal(t, allowed[path], found,
				"%s: the terminal reads this file is allowed are pinned; a removed one is worth noticing too", path)
			return nil
		}))
	}

	// And the whole tree's terminal reads, by file, so that a third one
	// anywhere in the shipped source has to be argued for here. The
	// census is over the full detection regexp and not over one spelling
	// of it: a rule that matched only `tool.IsTerminal(` could not see a
	// detection written any other way, which is exactly how the engine's
	// own os.ModeCharDevice sat outside every guard.
	//
	// Both are RENDERING questions — whether a guest shell can be
	// interactive, whether a diff should be paged — and both are below
	// the layer that decides who asked for anything.
	assert.Equal(t, map[string]int{
		// The primitive's own declaration, which reads nothing: it is the
		// function every caller below is counted by.
		"internal/tool/terminal.go":    1,
		"internal/verify/tart/tart.go": 1,
		"internal/engine/run.go":       1,
	}, spelledIn(t, detection.String()),
		"the terminal reads in the tree are about TTYs and pagers, not about an invoker")

	// The three declarations, and nothing else, decide.
	resolve := bodyOf(t, compiledFile(t, "cmd/auto.go"), "func resolveInvoker(")
	assert.Equal(t, 1, matches(resolve, `c\.Annotations\[autoVerbAnnotation\]`),
		"the auto verb declares itself by annotation, so a rename cannot unhook it")
	assert.Equal(t, 1, matches(resolve, `c\.Flags\(\)\.GetBool\(autoFlag\)`))
	assert.Equal(t, 1, matches(resolve, `autoFromEnv\(\)`))
	assert.Equal(t, 2, matches(resolve, `return record\.Machine`),
		"a machine is declared by the verb or by the flag-and-environment pair, and by nothing else")
	assert.Equal(t, 1, matches(resolve, `return record\.Human`),
		"and everything that declared nothing is a person")

	auto := compiledFile(t, "cmd/auto.go")
	assert.Equal(t, 1, matches(auto, `autoEnv\s+= "DOCKHAND_AUTO"`))
	assert.Equal(t, 1, matches(auto, `agentEnv = "AI_AGENT"`),
		"the agent marker is recorded beside the declaration and read by no gate")

	heldBy(t, "cmd/auto_test.go", "TestAutoModeIsDeclaredAndNeverDetected")
	heldBy(t, "cmd/auto_test.go", "TestNoInvokerPathDetectsATerminal")
}

// THE RULING: the machine gate refuses on this build.
//
// Ring 3 is other people's attention. The unattended road that would
// spend it is built, gated and tested; what has NOT been ruled on is the
// trust ladder that would say how many pull requests a machine may open,
// on which ports, at what pace, and on what record of past merges. Until
// that exists the answer is no, and the answer is a build-time constant
// rather than a flag so that no invocation, no environment variable and
// no configuration file can be the thing that changed it.
//
// There is deliberately no test anywhere that an unattended publication
// WORKS. On this build it cannot, and a test asserting the happy path
// would be pinning behaviour no binary has.
//
// Exercised by TestTheMachineGateRefusesOnThisBuild in internal/engine
// and TestThePermissionTravelsAsAValueAndDefaultsToRefusing in
// internal/cmd, which builds the shipped tree and shows it refusing.
func TestTheMachineGateRefusesOnThisBuild(t *testing.T) {
	// The constant, spelled false, in the one file that holds it.
	assert.Equal(t, 1, matches(compiledFile(t, "cmd/machinepublish.go"), `^const machinePublishEnabled = false$`),
		"ring 3 is unspendable by any machine on this build; flipping this is the trust ladder's ruling to make")
	assert.Equal(t, map[string]int{
		"internal/cmd/machinepublish.go": 1,
		// Spent once, at the composition root, into the run's context —
		// from where it travels as a value into the engine's Deps.
		"internal/cmd/root.go": 1,
	}, spelledIn(t, `machinePublishEnabled`),
		"one constant, one spend: a second reader is a second thing to flip")

	// The band it refuses in. It is a REFUSAL and not a failure: the
	// change is fine, the road refused it, and a person asking for the
	// same thing would be allowed it. That is the definition of the code.
	assert.Equal(t, 24, exitcode.MachineGate)
	assert.Equal(t, "refused", exitcode.Family(exitcode.MachineGate))

	// And the judgment above the gate, which is verdict's own and is
	// therefore exercised: an unattended publication with no positive
	// evidence is refused at that same code, so the two layers agree
	// about what a machine gets told.
	d := DecidePublish(PublishAsk{Record: set(nil), Branch: "dockhand/jq", Tip: "abc1234",
		By: record.Machine, Phase: PhaseInFlight})
	require.Error(t, d.Refusal)
	var coder exitcode.Coder
	require.ErrorAs(t, d.Refusal, &coder)
	assert.Equal(t, exitcode.MachineGate, coder.DockhandExit())
	assert.False(t, d.SayUnverified, "the machine road refuses; it does not complain and proceed")

	heldBy(t, "engine/machinegate_test.go", "TestTheMachineGateRefusesOnThisBuild")
	heldBy(t, "engine/machinegate_test.go", "TestOnlyOneCallSiteSpendsRingThree")
	heldBy(t, "cmd/machinepublish_test.go", "TestThePermissionTravelsAsAValueAndDefaultsToRefusing")
}

// The source-reading half of the index. verdict may not import the
// layers that act, so the rulings that live above it are asserted
// against their own source — over the lines the compiler sees, so that a
// rule may be NAMED in a comment without the comment satisfying or
// failing the rule it describes.

// internalDir is internal/, which is this package's parent, and mainDir
// is the binary's own package beside it. Paths are named for a ruling
// the way a reader would say them, and a census covers both, because a
// rule that scanned only internal/ would be a rule with main.go for a
// hole.
const (
	internalDir = ".."
	mainDir     = "../../cmd"
)

// shipped is the Go source this repository builds into the binary, each
// root with the prefix a failure should name it by.
var shipped = []struct{ dir, prefix string }{
	{internalDir, "internal/"},
	{mainDir, "cmd/"},
}

// readFile reads one file or fails the ruling. A ruling pinned against a
// file that has been renamed is a ruling nobody is checking, so the
// absence is the failure rather than a skip.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err, "the ruling is pinned against %s", path)
	return string(b)
}

// compiledFile is one file under internal/, reduced to the lines the
// compiler sees.
func compiledFile(t *testing.T, rel string) []string {
	t.Helper()
	return compiledOnly(readFile(t, filepath.Join(internalDir, filepath.FromSlash(rel))))
}

// compiledOnly drops comments, so a structural rule can be stated in
// prose beside the code that obeys it.
func compiledOnly(src string) []string {
	var out []string
	block := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case block:
			if strings.Contains(trimmed, "*/") {
				block = false
			}
		case strings.HasPrefix(trimmed, "//"):
		case strings.HasPrefix(trimmed, "/*"):
			block = !strings.Contains(trimmed, "*/")
		default:
			out = append(out, line)
		}
	}
	return out
}

// bodyOf returns the lines inside the brace-delimited block that opens on
// the first line containing header — a function, or a guard inside one.
//
// Brace counting rather than a parser because go/ast is not on this
// package's import list and should not be: the depth is read over
// comment-free lines within one function, which is a few dozen lines of
// dockhand's own source rather than arbitrary input. A brace inside a
// string literal in one of those bodies would fool it, which is a thing
// to notice when a ruling here starts failing for no reason.
func bodyOf(t *testing.T, lines []string, header string) []string {
	t.Helper()
	start := -1
	for i, line := range lines {
		if strings.Contains(line, header) {
			start = i
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "%q is where the ruling is written, and it is not there", header)

	depth := strings.Count(lines[start], "{") - strings.Count(lines[start], "}")
	require.Positive(t, depth, "%q must open a block", header)
	for i := start + 1; i < len(lines); i++ {
		if depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}"); depth <= 0 {
			return lines[start+1 : i]
		}
	}
	t.Fatalf("%q opens a block that never closes", header)
	return nil
}

// matches counts the lines a pattern occurs on. Lines and not
// occurrences: every rule here is about a statement being written, and
// one line saying a thing twice is still one place it is said.
func matches(lines []string, pattern string) int {
	re := regexp.MustCompile(pattern)
	n := 0
	for _, line := range lines {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}

// spelledIn counts a pattern's occurrences per file across everything
// that ships, skipping tests.
//
// The answer is a map from file to count, and the rulings assert the
// whole map. A count alone would say a rule broke; the map says which
// file grew the second spelling, which is the sentence a reader of the
// failure actually needs.
func spelledIn(t *testing.T, pattern string) map[string]int {
	t.Helper()
	re := regexp.MustCompile(pattern)
	found := map[string]int{}
	for _, root := range shipped {
		require.NoError(t, filepath.WalkDir(root.dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel := root.prefix + strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(root.dir)+"/")
			for _, line := range compiledOnly(readFile(t, path)) {
				if re.MatchString(line) {
					found[rel]++
				}
			}
			return nil
		}))
	}
	return found
}

// heldBy requires that the test which actually EXERCISES a ruling still
// exists, by name, in the package that can run it.
//
// This is what keeps the index honest. A structural assertion says the
// guard is still written; it cannot say the guard still works. The proof
// of that lives where the code can be executed, and this file's claim to
// index the rulings would be worthless if that proof could be deleted
// without anything noticing.
func heldBy(t *testing.T, rel, name string) {
	t.Helper()
	src := readFile(t, filepath.Join(internalDir, filepath.FromSlash(rel)))
	assert.Contains(t, src, "func "+name+"(t *testing.T)",
		"%s is where this ruling is exercised; the index pins the guard, that test proves it", rel)
}

// D24 — the dependents are best effort, on both roads; an outcome is
// about the port, not the machine; the evidence is read before the
// cancel (2026-09-04).
//
// EXERCISED where verdict and record own the judgment: a cohort whose
// dependent failed publishes, on the human road and the machine road
// alike; one whose dependent errored or was canceled does not, because
// neither is an outcome about the port; a failed headline never
// publishes. SOURCE for the ordering the engine owns: promote reads the
// evidence before it cancels anything, so the gate never judges canceled
// runs the promotion itself just wrote. HELD BY the engine and render
// tests that run those roads.
func TestDependentsAreBestEffortAndAnOutcomeIsAboutThePort(t *testing.T) {
	cohort := func(dep record.RunState) record.Record {
		return record.Record{
			Subjects: []record.Subject{{Port: "libraw"}, {Port: "gthumb"}},
			Runs: map[string]record.Run{
				record.RunKey("libraw", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
				record.RunKey("gthumb", "Sequoia"): {State: dep, Platform: "Sequoia"},
			},
		}
	}
	for _, by := range []record.Driver{record.Human, record.Machine} {
		n := cohort(record.Failed)
		d := DecidePublish(PublishAsk{Record: n, Promotable: n.Promotable(),
			Branch: "b", Tip: "t", By: by, Phase: PhaseInFlight})
		require.NoError(t, d.Refusal, "%s: a dependent that failed is published over and named", by)
	}
	for _, st := range []record.RunState{record.Errored, record.Canceled, record.Superseded} {
		assert.False(t, cohort(st).Promotable(), "%s is not an outcome about the port", st)
	}
	assert.False(t, record.Record{
		Subjects: []record.Subject{{Port: "libraw"}, {Port: "gthumb"}},
		Runs: map[string]record.Run{
			record.RunKey("libraw", "Sequoia"): {State: record.Failed, Platform: "Sequoia"},
			record.RunKey("gthumb", "Sequoia"): {State: record.Passed, Platform: "Sequoia"},
		},
	}.Promotable(), "the headline is not best effort")

	// The evidence is read before the cancel: EvidenceFor precedes
	// cancelRuns in the body of Promote.
	body := bodyOf(t, compiledFile(t, "engine/promote.go"), "func (e *Engine) Promote(")
	evidence, cancel := -1, -1
	for i, line := range body {
		if evidence < 0 && matches([]string{line}, `EvidenceFor\(ctx, tip\)`) == 1 {
			evidence = i
		}
		if cancel < 0 && matches([]string{line}, `cancelRuns\(ctx, repo, tip`) == 1 {
			cancel = i
		}
	}
	require.NotEqual(t, -1, evidence)
	require.NotEqual(t, -1, cancel)
	assert.Less(t, evidence, cancel, "the record is read, then judged, and only then are builds stopped")

	heldBy(t, "cmd/promote_lifecycle_test.go", "TestPromoteMidVerificationCancelsAndProceeds")
	heldBy(t, "render/body_test.go", "TestPRBodyKeepsADependentsFailureBesideItsPass")
	heldBy(t, "verdict/machineroad_test.go", "TestAMachinePublishesACohortWhoseDependentFailed")
}

// D25 — a member behind a failed prerequisite is blocked, and the judge
// trusts the guest's per-member state files (2026-09-04).
//
// EXERCISED, both halves. A member the runner skipped because a member
// it requires failed is blocked, blamed on that member, in the
// sibling's words — and it is the runner's own record that says so:
// the log is silent about a skipped member and silent about one the
// runner never reached, and the same log with and without the record
// settles the silent member two different ways. SOURCE for the trust:
// the comment that parked the state-file question records the answer
// where it was parked, and the runner reads the record back through
// verify.MemberStater. HELD BY the runner's behavioural tests in
// verify/tart.
func TestAMemberBehindAFailedPrerequisiteIsBlockedAndStateFilesAreTrusted(t *testing.T) {
	in := CohortInput{
		Subjects: []record.Subject{{Port: "oniguruma6"}, {Port: "jq"}},
		Runs: map[string]record.Run{
			"oniguruma6": {State: record.Running, Platform: "Tahoe"},
			"jq":         {State: record.Running, Platform: "Tahoe"},
		},
		Status:  verify.Status{State: verify.Failed, Handle: "w"},
		Log:     "===> dockhand subject: oniguruma6\nError: Failed to build oniguruma6: command execution failed\n",
		LogRead: true,
		Reported: []verify.MemberState{
			{Port: "oniguruma6", Outcome: verify.MemberFailed},
			{Port: "jq", Outcome: verify.MemberSkipped, Prerequisite: "oniguruma6"},
		},
	}
	out := JudgeCohort(in)
	assert.Equal(t, record.Failed, out["oniguruma6"].Run.State)
	assert.Equal(t, record.Blocked, out["jq"].Run.State, "blocked, not withheld: something the change is responsible for failed")
	assert.Equal(t, "oniguruma6", out["jq"].Run.Blamed)
	assert.Contains(t, out["jq"].Run.Detail, "this member is untested")

	// The trust half: the same log without the record cannot tell a
	// member skipped on purpose from one a dying runner never reached,
	// and says so rather than guessing.
	in.Reported = nil
	assert.Equal(t, record.Errored, JudgeCohort(in)["jq"].Run.State,
		"the log alone reads jq's silence as a runner that did not finish")

	tart := strings.Split(readFile(t, filepath.Join(internalDir, "verify/tart/tart.go")), "\n")
	assert.Equal(t, 1, matches(tart, `may trust them \(maintainer's ruling, 2026-09-04\)`),
		"the parked question carries its answer where it was parked")
	heldBy(t, "verify/tart/guest_test.go", "TestACohortSkipsAMemberWhosePrerequisiteFailed")
	heldBy(t, "verify/tart/guest_test.go", "TestACohortGoesOnPastAMemberThatFailedWhenNothingDependsOnIt")
}

// D26 — the audit row says what a promotion carried (2026-09-04).
//
// EXERCISED: the members a promotion publishes without a pass are one
// reading — the lines the author sees and the count the row carries —
// and it is not proven's complement: withheld counts, unsupported does
// not. SOURCE: promote spells the count once, from that reading. HELD
// BY the engine test that reads the row back.
func TestTheAuditRowSaysWhatAPromotionCarried(t *testing.T) {
	r := record.Record{
		Subjects: []record.Subject{{Port: "libraw"}, {Port: "gegl-devel"}, {Port: "gthumb"}, {Port: "geeqie"}},
		Runs: map[string]record.Run{
			record.RunKey("libraw", "Tahoe"):     {State: record.Passed, Platform: "Tahoe"},
			record.RunKey("gegl-devel", "Tahoe"): {State: record.Withheld, Platform: "Tahoe"},
			record.RunKey("gthumb", "Tahoe"):     {State: record.Failed, Platform: "Tahoe"},
			record.RunKey("geeqie", "Tahoe"):     {State: record.Unsupported, Platform: "Tahoe"},
		},
	}
	assert.Equal(t, []string{"gegl-devel", "gthumb"}, r.UnprovenMembers())
	assert.True(t, r.Promotable(), "and the change publishes, which is why the count exists")
	assert.Len(t, DependentsNotProven(r), 2, "the author is told the same two")

	assert.Equal(t, map[string]int{"internal/engine/promote.go": 1}, spelledIn(t, `Unproven: len\(n\.UnprovenMembers\(\)\)`),
		"one writer, from the one reading")
	heldBy(t, "engine/outcome_test.go", "TestPublishRecordsHowManyMembersWereUnproven")
}
